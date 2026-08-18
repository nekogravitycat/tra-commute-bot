package tdx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
)

type stationsResponse struct {
	UpdateTime string        `json:"UpdateTime"`
	Stations   []stationItem `json:"Stations"`
}

type stationItem struct {
	StationID   string   `json:"StationID"`
	StationName nameZhEn `json:"StationName"`
}

// Stations fetches the full TRA station catalog from /Station.
//
// The running program must never call this: §6.2 restricts /Station to
// build time, so that a normal tick never risks the rate limit (§6.5) on a
// request that has nothing to do with today's brief. It exists solely for
// cmd/gen-stations, which calls it once to regenerate
// internal/domain/stations_data.go — the file /setup's station matcher
// (domain.MatchStations) actually reads at runtime.
func (c *Client) Stations(ctx context.Context) ([]domain.Station, error) {
	body, err := c.get(ctx, "stations", "/Station")
	if err != nil {
		return nil, fmt.Errorf("tdx stations: %w", err)
	}
	var resp stationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("tdx stations: decode: %w", err)
	}
	out := make([]domain.Station, 0, len(resp.Stations))
	for _, s := range resp.Stations {
		out = append(out, domain.Station{ID: s.StationID, NameZh: s.StationName.ZhTw, NameEn: s.StationName.En})
	}
	return out, nil
}
