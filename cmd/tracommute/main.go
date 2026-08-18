// Command tracommute produces the daily TRA commute brief.
//
// It is woken once a minute by a systemd timer and decides for itself whether
// this minute is the scheduled one (§10.3). Almost every invocation exits
// within milliseconds without touching the network.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	// Embedding the timezone database removes the single most annoying
	// deployment failure for a program whose entire job is schedule-sensitive:
	// a minimal host image without tzdata, where LoadLocation would fail and
	// every time in the brief would silently be UTC.
	_ "time/tzdata"

	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/archive"
	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/render"
	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/settingsfile"
	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/statefile"
	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/tdx"
	"github.com/nekogravitycat/tra-commute-bot/internal/adapter/telegram"
	"github.com/nekogravitycat/tra-commute-bot/internal/config"
	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
	"github.com/nekogravitycat/tra-commute-bot/internal/platform/clock"
	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

const (
	defaultConfigPath = "/etc/tra-commute/config.yaml"
	atLayout          = "2006-01-02T15:04"
	// sendRetries and sendBackoff implement the §9.3 delivery policy: three
	// extra attempts with a doubling wait before the run is declared failed.
	sendRetries = 3
	sendBackoff = 2 * time.Second
)

// version is set at build time via -ldflags. When a brief looks wrong, the
// first question is which build produced it.
var version = "dev"

type options struct {
	configPath  string
	at          string
	dotEnv      string
	dryRun      bool
	force       bool
	verbose     bool
	showVersion bool
}

func main() {
	if err := run(); err != nil {
		slog.Error("run failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var o options
	flag.StringVar(&o.configPath, "config", defaultConfigPath, "設定檔路徑")
	flag.StringVar(&o.at, "at", "", `模擬指定時刻執行，格式 "2006-01-02T15:04"（測試用）`)
	flag.StringVar(&o.dotEnv, "env-file", ".env", "本機開發用的 KEY=VALUE 憑證檔（不存在則忽略）")
	flag.BoolVar(&o.dryRun, "dry-run", false, "計算並印出訊息到 stdout，不發送 Telegram，不寫入狀態")
	flag.BoolVar(&o.force, "force", false, "略過排程判斷，直接以第一個 schedule 的時刻執行一次")
	flag.BoolVar(&o.verbose, "verbose", false, "輸出 debug 等級日誌與完整 API 回應")
	flag.BoolVar(&o.showVersion, "version", false, "印出版本後結束")
	flag.Parse()

	if o.showVersion {
		fmt.Println("tracommute", version)
		return nil
	}

	log := newLogger(o.verbose, o.dryRun)
	slog.SetDefault(log)
	log.Debug("starting", "version", version)

	if err := config.LoadDotEnv(o.dotEnv); err != nil {
		log.Warn("could not read env file", "path", o.dotEnv, "err", err)
	}
	cfg, err := config.Load(o.configPath)
	if err != nil {
		return err
	}
	creds, err := config.LoadCredentials()
	if err != nil {
		return err
	}
	cfg.Credentials = creds

	now, err := resolveClock(o, cfg.Location)
	if err != nil {
		return err
	}

	app, err := build(o, cfg, log)
	if err != nil {
		return err
	}
	defer app.prune(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if o.force {
		return app.forceRun(ctx, now, o, log)
	}

	res, err := app.tick(now, o, log).Run(ctx)
	if err != nil {
		return err
	}
	report(res.Result, res.Ran, o, log)
	return nil
}

// application holds the wired dependencies.
type application struct {
	brief    *usecase.Brief
	state    usecase.StateStore
	settings usecase.SettingsStore
	guard    guardParams
	archive  *archive.Dir
}

// guardParams are the admin-only scheduling knobs that stay in config.yaml:
// they change rarely, unlike the schedule's own weekdays and fire time,
// which the user edits live via /schedule and which usecase.Tick reads out
// of SettingsStore on every wake-up.
type guardParams struct {
	SkipDates   []string
	ExtraDates  []string
	Tolerance   time.Duration
	RetryWindow time.Duration
}

// build is the composition root: the one place that knows which concrete
// adapter satisfies which port. Nothing below this function imports another
// adapter, which is what keeps the dependency arrows pointing inwards.
func build(o options, cfg config.Config, log *slog.Logger) (*application, error) {
	var arch *archive.Dir
	if !o.dryRun && cfg.ArchiveDir != "" {
		arch = archive.New(cfg.ArchiveDir, cfg.ArchiveRetention, time.Now)
	}

	tdxOpts := tdx.Options{
		Interval: cfg.RequestInterval,
		Timeout:  cfg.RequestTimeout,
		Log:      log,
	}
	if arch != nil {
		tdxOpts.Archiver = func(name string, payload []byte) {
			if err := arch.Archive(name, payload); err != nil {
				log.Warn("archive write failed", "request", name, "err", err)
			}
		}
	}
	if o.verbose {
		// Chained onto the archiver hook, which fires only for data responses.
		// The token response never reaches either, so -verbose cannot print a
		// bearer credential into the journal.
		inner := tdxOpts.Archiver
		tdxOpts.Archiver = func(name string, payload []byte) {
			log.Debug("tdx response", "request", name, "body", string(payload))
			if inner != nil {
				inner(name, payload)
			}
		}
	}

	client := tdx.New(tdx.Credentials{
		ClientID:     cfg.Credentials.TDXClientID,
		ClientSecret: cfg.Credentials.TDXClientSecret,
	}, cfg.Location, tdxOpts)

	notifier, err := buildNotifier(o, cfg, log)
	if err != nil {
		return nil, err
	}

	brief := &usecase.Brief{
		Timetable: &authOnDemand{client: client},
		Delays:    &authOnDemand{client: client},
		Renderer: render.Telegram{
			MaxAlternatives: cfg.MaxAlternatives,
			CertificateNote: cfg.CertificateNote,
		},
		Notifier:    notifier,
		Log:         log,
		SendRetries: sendRetries,
		SendBackoff: sendBackoff,
		Settings: usecase.BriefSettings{
			Board:               cfg.Board,
			RiskMargin:          cfg.RiskMargin,
			Window:              cfg.Window,
			Filter:              cfg.Filter,
			UsualTrainNos:       cfg.UsualTrainNos,
			CertificateEnabled:  cfg.CertificateEnabled,
			CertificateMinDelay: cfg.CertificateMinDelay,
			CompensationEnabled: cfg.CompensationEnabled,
			SevereThreshold:     cfg.SevereThreshold,
		},
	}

	return &application{
		brief:    brief,
		state:    statefile.New(cfg.StatePath),
		settings: settingsfile.New(cfg.SettingsPath),
		guard: guardParams{
			SkipDates:   cfg.SkipDates,
			ExtraDates:  cfg.ExtraDates,
			Tolerance:   cfg.Tolerance,
			RetryWindow: cfg.RetryWindow,
		},
		archive: arch,
	}, nil
}

func buildNotifier(o options, cfg config.Config, log *slog.Logger) (usecase.Notifier, error) {
	if o.dryRun {
		return stdoutNotifier{}, nil
	}
	if !cfg.Credentials.TelegramConfigured() {
		return nil, fmt.Errorf("missing credentials: %s, %s (aliases TG_BOT_TOKEN / TG_CHAT_ID also accepted; or use -dry-run)",
			config.EnvTelegramToken, config.EnvTelegramChatID)
	}
	return telegram.New(telegram.Config{
		BotToken: cfg.Credentials.TelegramToken,
		ChatID:   cfg.Credentials.TelegramChatID,
		Timeout:  cfg.RequestTimeout,
	}), nil
}

func (a *application) tick(now time.Time, o options, log *slog.Logger) *usecase.Tick {
	return &usecase.Tick{
		Clock:       clock.Fixed{At: now},
		State:       a.state,
		Settings:    a.settings,
		Brief:       a.brief,
		Log:         log,
		SkipDates:   a.guard.SkipDates,
		ExtraDates:  a.guard.ExtraDates,
		Tolerance:   a.guard.Tolerance,
		RetryWindow: a.guard.RetryWindow,
		DryRun:      o.dryRun,
	}
}

// forceRun bypasses the schedule guard entirely, using the live settings'
// schedule time on the simulated date. Without it, a mid-afternoon debugging
// session produces nothing at all, because the guard is doing its job.
func (a *application) forceRun(ctx context.Context, now time.Time, o options, log *slog.Logger) error {
	trip, err := a.settings.Load()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	complete, missing := trip.Complete()
	if !complete {
		return fmt.Errorf("-force needs the live settings to be complete first; missing: %s "+
			"(use Telegram's /route /ready /deadline /schedule /earlyleave)", strings.Join(missing, ", "))
	}

	sch := trip.Schedule()
	firedAt := sch.At.On(now)
	log.Info("forced run", "schedule", sch.Name, "fired_at", firedAt)

	res, err := a.brief.Run(ctx, firedAt, sch.Name, trip)
	if err != nil {
		return err
	}
	report(res, true, o, log)
	return nil
}

func (a *application) prune(log *slog.Logger) {
	if a.archive == nil {
		return
	}
	if err := a.archive.Prune(); err != nil {
		log.Warn("archive prune failed", "err", err)
	}
}

func report(res usecase.Result, ran bool, o options, log *slog.Logger) {
	if !ran {
		return
	}
	if o.dryRun {
		fmt.Println(res.Message.Text)
		return
	}
	log.Info("brief delivered",
		"mode", res.Brief.Mode.String(),
		"recommended", recommendedNo(res.Brief),
		"candidates", len(res.Brief.Plan.Candidates))
}

func recommendedNo(b domain.Brief) string {
	if b.Plan.Recommended == nil {
		return ""
	}
	return b.Plan.Recommended.TrainNo
}

// authOnDemand fetches a token before the first API call of the process.
//
// The token lives 24 hours but the process lives seconds, so caching it to disk
// would add a file and its permissions to save one request in three.
type authOnDemand struct {
	client *tdx.Client
	done   bool
}

func (a *authOnDemand) ensure(ctx context.Context) error {
	if a.done {
		return nil
	}
	if err := a.client.Authenticate(ctx); err != nil {
		return err
	}
	a.done = true
	return nil
}

func (a *authOnDemand) DailyODTimetable(ctx context.Context, originID, destID string, date time.Time) (usecase.Timetable, error) {
	if err := a.ensure(ctx); err != nil {
		return usecase.Timetable{}, err
	}
	return a.client.DailyODTimetable(ctx, originID, destID, date)
}

func (a *authOnDemand) LiveDelays(ctx context.Context) (usecase.DelaySnapshot, error) {
	if err := a.ensure(ctx); err != nil {
		return usecase.DelaySnapshot{}, err
	}
	return a.client.LiveDelays(ctx)
}

// stdoutNotifier backs -dry-run. Delivery is a no-op here because the message
// is printed by report(), which keeps the output to exactly one copy.
type stdoutNotifier struct{}

func (stdoutNotifier) Send(context.Context, usecase.Message) error { return nil }

func resolveClock(o options, loc *time.Location) (time.Time, error) {
	if o.at == "" {
		return clock.Real{Loc: loc}.Now(), nil
	}
	t, err := time.ParseInLocation(atLayout, o.at, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("-at %q must look like %s: %w", o.at, atLayout, err)
	}
	return t, nil
}

func newLogger(verbose, dryRun bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	// JSON to stdout, which journald ingests as structured fields. Under
	// -dry-run the logs move to stderr so that stdout carries the rendered
	// message and nothing else, and can be redirected to a file or a diff.
	out := os.Stdout
	if dryRun {
		out = os.Stderr
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level}))
}
