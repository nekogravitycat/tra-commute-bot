package tdx

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nekogravitycat/tra-commute-bot/internal/domain"
	"github.com/nekogravitycat/tra-commute-bot/internal/usecase"
)

// DailyODTimetable fetches the day's origin-to-destination services.
//
// The endpoint requires an explicit yyyy-MM-dd date; there is no /Today
// variant, and yyyy/MM/dd is rejected outright.
func (c *Client) DailyODTimetable(ctx context.Context, originID, destID string, date time.Time) (usecase.Timetable, error) {
	path := fmt.Sprintf("/DailyTrainTimetable/OD/%s/to/%s/%s", originID, destID, date.Format("2006-01-02"))
	body, err := c.get(ctx, "timetable", path)
	if err != nil {
		return usecase.Timetable{}, fmt.Errorf("tdx timetable: %w", err)
	}

	var resp odTimetableResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return usecase.Timetable{}, fmt.Errorf("tdx timetable: decode: %w", err)
	}

	serviceDate, err := parseDate(resp.TrainDate, c.loc)
	if err != nil {
		// Falling back to the requested date keeps a malformed TrainDate from
		// taking down the whole run; the two agree in every observed response.
		c.opts.Log.Warn("unparsable TrainDate, using requested date",
			"train_date", resp.TrainDate, "err", err)
		serviceDate = date
	}

	updatedAt, err := parseTimestamp(resp.UpdateTime, c.loc)
	if err != nil {
		// Metadata, not a reason to abandon an otherwise good timetable — the
		// renderer treats a zero UpdatedAt as "unknown" and omits it.
		c.opts.Log.Warn("unparsable UpdateTime, leaving it unset", "value", resp.UpdateTime, "err", err)
	}

	out := usecase.Timetable{
		ServiceDate: serviceDate,
		UpdatedAt:   updatedAt,
		Services:    make([]domain.Service, 0, len(resp.TrainTimetables)),
	}
	for _, tt := range resp.TrainTimetables {
		svc, ok := c.toService(tt, serviceDate, originID, destID)
		if !ok {
			continue
		}
		out.Services = append(out.Services, svc)
	}
	return out, nil
}

// toService maps one timetable entry onto a domain service, resolving the naive
// clocks against the service date.
func (c *Client) toService(tt trainTimetable, serviceDate time.Time, originID, destID string) (domain.Service, bool) {
	var origin, dest *stopTime
	for i := range tt.StopTimes {
		switch tt.StopTimes[i].StationID {
		case originID:
			origin = &tt.StopTimes[i]
		case destID:
			dest = &tt.StopTimes[i]
		}
	}
	if origin == nil || dest == nil {
		return domain.Service{}, false
	}
	// The OD endpoint should never return the destination ahead of the
	// origin, but the direction is worth confirming before the times are
	// interpreted as a leg.
	if origin.StopSequence >= dest.StopSequence {
		return domain.Service{}, false
	}

	dep, err := resolveClock(serviceDate, origin.DepartureTime)
	if err != nil {
		c.opts.Log.Warn("skipping train with unparsable departure",
			"train", tt.TrainInfo.TrainNo, "value", origin.DepartureTime, "err", err)
		return domain.Service{}, false
	}
	arr, err := resolveClock(serviceDate, dest.ArrivalTime)
	if err != nil {
		c.opts.Log.Warn("skipping train with unparsable arrival",
			"train", tt.TrainInfo.TrainNo, "value", dest.ArrivalTime, "err", err)
		return domain.Service{}, false
	}
	// The timetable publishes clock times with no date, so a train that runs
	// past midnight (23:28 → 00:02 exists on this line) would otherwise appear
	// to arrive before it departs. Since the OD endpoint returns only these
	// two stops, one comparison settles it.
	if !arr.After(dep) {
		arr = arr.AddDate(0, 0, 1)
	}

	return domain.Service{
		TrainNo:  tt.TrainInfo.TrainNo,
		TypeID:   tt.TrainInfo.TrainTypeID,
		TypeCode: tt.TrainInfo.TrainTypeCode,
		TypeName: tt.TrainInfo.TrainTypeName.ZhTw,
		SchedDep: dep,
		SchedArr: arr,
		Suspended: tt.TrainInfo.SuspendedFlag != 0 ||
			origin.SuspendedFlag != 0 || dest.SuspendedFlag != 0,
	}, true
}
