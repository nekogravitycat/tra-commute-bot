package tdx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

// LiveDelays fetches the delay of every running train in one request.
//
// This is the only source of delay data. The per-station board was dropped
// after it returned a single row for a busy station — the whole-network board
// covers the same need in one call, with one set of field names and one time
// format to get wrong instead of two.
func (c *Client) LiveDelays(ctx context.Context) (usecase.DelaySnapshot, error) {
	body, err := c.get(ctx, "liveboard", "/TrainLiveBoard")
	if err != nil {
		return usecase.DelaySnapshot{}, fmt.Errorf("tdx live board: %w", err)
	}

	var resp liveBoardResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return usecase.DelaySnapshot{}, fmt.Errorf("tdx live board: decode: %w", err)
	}

	snap := usecase.DelaySnapshot{
		UpdatedAt: parseTimestamp(resp.UpdateTime, c.loc),
		ByTrainNo: make(map[string]int, len(resp.TrainLiveBoards)),
	}
	for _, item := range resp.TrainLiveBoards {
		if item.TrainNo == "" {
			continue
		}
		snap.ByTrainNo[item.TrainNo] = item.DelayTime
	}
	return snap, nil
}
