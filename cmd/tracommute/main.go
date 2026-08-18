// Command tracommute produces the daily TRA commute brief.
//
// The default invocation is a long-running process (§4.2): a notify loop
// wakes once a minute and decides for itself whether any Schedule is due
// (§10.8), and a command loop long-polls Telegram for /setup, /manage and
// the rest of §10's commands. -at, -dry-run and -force instead run a single
// simulated tick and exit, which is how a program that otherwise runs all
// day is debugged without waiting for the clock.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
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
	"github.com/nekogravitycat/tra-commute-bot/internal/command"
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
	// pollTimeoutSeconds is how long each getUpdates long poll holds the
	// connection open, within the Bot API's recommended 30-50s (§5.2).
	pollTimeoutSeconds = 40
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
	flag.StringVar(&o.at, "at", "", `模擬指定時刻執行單一次 tick，格式 "2006-01-02T15:04"（測試用）`)
	flag.StringVar(&o.dotEnv, "env-file", ".env", "本機開發用的 KEY=VALUE 憑證檔（不存在則忽略）")
	flag.BoolVar(&o.dryRun, "dry-run", false, "計算並印出訊息到 stdout，不發送 Telegram，不寫入狀態")
	flag.BoolVar(&o.force, "force", false, "略過排程判斷，對已完成設定的第一條 Schedule 立即執行一次")
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

	app, err := build(o, cfg, log)
	if err != nil {
		return err
	}
	defer app.prune(log)

	// -at, -dry-run and -force are the one-shot debugging path (§10.11): a
	// single simulated tick, printed or delivered, then exit. Their absence
	// is what selects the real, default invocation — the long-running
	// notify-loop-plus-command-loop process (§4.2) that the rest of the day
	// actually runs as.
	if o.at != "" || o.dryRun || o.force {
		now, err := resolveClock(o, cfg.Location)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if o.force {
			return app.forceRun(ctx, now, o, log)
		}
		res, err := app.tick(now, app.settingsStore, o, log).Run(ctx)
		if err != nil {
			return err
		}
		report(res, o, log)
		return nil
	}

	return app.serve(log)
}

// application holds the wired dependencies.
type application struct {
	brief         *usecase.Brief
	state         usecase.StateStore
	settingsStore usecase.SettingsStore // the raw file-backed store
	settingsActor *usecase.SettingsActor
	router        *command.Router // nil when Telegram is not configured (never true outside -dry-run)
	guard         guardParams
	archive       *archive.Dir
	loc           *time.Location
}

// guardParams are the admin-only scheduling knobs that stay in config.yaml:
// they change rarely, unlike each Schedule's own weekdays and fire time,
// which the user edits live via /setup and /manage and which usecase.Tick
// reads out of the settings store on every wake-up.
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

	bot, err := buildBot(o, cfg)
	if err != nil {
		return nil, err
	}
	notifier := usecase.Notifier(stdoutNotifier{})
	if bot != nil {
		notifier = bot
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

	settingsStore := settingsfile.New(cfg.SettingsPath)
	stateStore := statefile.New(cfg.StatePath)
	actor := usecase.NewSettingsActor(settingsStore, log)

	var router *command.Router
	if bot != nil {
		router = command.NewRouter(bot, actor, stateStore, domain.AllStations, cfg.Credentials.TelegramChatID, log)
	}

	return &application{
		brief:         brief,
		state:         stateStore,
		settingsStore: settingsStore,
		settingsActor: actor,
		router:        router,
		guard: guardParams{
			SkipDates:   cfg.SkipDates,
			ExtraDates:  cfg.ExtraDates,
			Tolerance:   cfg.Tolerance,
			RetryWindow: cfg.RetryWindow,
		},
		archive: arch,
		loc:     cfg.Location,
	}, nil
}

// buildBot returns the concrete Telegram client, or nil under -dry-run,
// where nothing is ever sent and no command loop runs — see §10.11's note
// that -dry-run exists to check a message's content, not to exercise the
// live-settings interface.
func buildBot(o options, cfg config.Config) (*telegram.Notifier, error) {
	if o.dryRun {
		return nil, nil
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

func (a *application) tick(now time.Time, settings usecase.SettingsStore, o options, log *slog.Logger) *usecase.Tick {
	return &usecase.Tick{
		Clock:       clock.Fixed{At: now},
		State:       a.state,
		Settings:    settings,
		Brief:       a.brief,
		Log:         log,
		SkipDates:   a.guard.SkipDates,
		ExtraDates:  a.guard.ExtraDates,
		Tolerance:   a.guard.Tolerance,
		RetryWindow: a.guard.RetryWindow,
		DryRun:      o.dryRun,
	}
}

// forceRun bypasses the schedule guard entirely, running the first
// fully-configured Schedule at its own notify time on the simulated date.
// Without it, a mid-afternoon debugging session produces nothing at all,
// because the guard is doing its job.
func (a *application) forceRun(ctx context.Context, now time.Time, o options, log *slog.Logger) error {
	list, err := a.settingsStore.Load()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	if len(list.Schedules) == 0 {
		return fmt.Errorf("-force needs at least one Schedule to exist first — use Telegram's /setup")
	}
	trip := list.Schedules[0]

	sch := trip.Schedule()
	firedAt := sch.At.On(now)
	log.Info("forced run", "schedule", sch.Name, "fired_at", firedAt)

	res, err := a.brief.Run(ctx, firedAt, sch.Name, trip)
	if err != nil {
		return err
	}
	reportOne(res, o, log)
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

// serve runs the long-running process (§4.2, §5): the notify loop and the
// command loop, side by side, sharing settings.json through the single
// settings actor goroutine (§10.9), until SIGTERM or SIGINT asks for a
// graceful shutdown.
func (a *application) serve(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.settingsActor.Run(ctx)
	}()

	settings := usecase.ActorStore{Actor: a.settingsActor, Ctx: ctx}

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runNotifyLoop(ctx, settings, log)
	}()

	if a.router != nil {
		if err := a.router.SetMyCommands(ctx); err != nil {
			log.Warn("setMyCommands failed", "err", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			a.runCommandLoop(ctx, log)
		}()
	}

	log.Info("tracommute started", "version", version)
	<-ctx.Done()
	log.Info("shutting down")
	wg.Wait()
	return nil
}

// runNotifyLoop is the notify loop of §5: a tick immediately on startup (so
// a container restarted mid-tolerance-window does not have to wait a full
// minute for its first chance to catch up), then one every minute
// thereafter, until ctx is cancelled.
func (a *application) runNotifyLoop(ctx context.Context, settings usecase.SettingsStore, log *slog.Logger) {
	a.runTick(ctx, settings, log)

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runTick(ctx, settings, log)
		}
	}
}

func (a *application) runTick(ctx context.Context, settings usecase.SettingsStore, log *slog.Logger) {
	tickCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	now := clock.Real{Loc: a.loc}.Now()
	t := a.tick(now, settings, options{}, log)
	res, err := t.Run(tickCtx)
	if err != nil {
		log.Error("tick run failed", "err", err)
	}
	for _, out := range res.Outcomes {
		log.Info("brief delivered",
			"schedule", out.Decision.Schedule.Name,
			"mode", out.Result.Brief.Mode.String(),
			"recommended", recommendedNo(out.Result.Brief),
			"candidates", len(out.Result.Brief.Plan.Candidates))
	}
}

// runCommandLoop is the command loop of §5.2: getUpdates long-polling,
// dispatched to the Router, until ctx is cancelled. A getUpdates failure
// (a network blip, most likely) waits a few seconds rather than retrying in
// a hot loop, since unlike the notify loop this one has no minute-long gap
// between attempts built in.
func (a *application) runCommandLoop(ctx context.Context, log *slog.Logger) {
	offset := 0
	for {
		if ctx.Err() != nil {
			return
		}
		updates, err := a.router.Bot.GetUpdates(ctx, offset, pollTimeoutSeconds)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("getUpdates failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			a.router.HandleUpdate(ctx, u)
		}
	}
}

func report(res usecase.TickResult, o options, log *slog.Logger) {
	if !res.Ran() {
		return
	}
	for _, out := range res.Outcomes {
		reportOne(out.Result, o, log)
	}
}

func reportOne(res usecase.Result, o options, log *slog.Logger) {
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
// The token lives 24 hours but a one-shot invocation lives seconds, and the
// long-running process makes at most a handful of ticks an hour, so caching
// it to disk would add a file and its permissions to save very little.
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
// is printed by reportOne(), which keeps the output to exactly one copy.
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
	// JSON to stdout, which journald (or the container runtime's log driver)
	// ingests as structured fields. Under -dry-run the logs move to stderr so
	// that stdout carries the rendered message and nothing else, and can be
	// redirected to a file or a diff.
	out := os.Stdout
	if dryRun {
		out = os.Stderr
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level}))
}
