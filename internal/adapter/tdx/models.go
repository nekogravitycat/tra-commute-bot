// Package tdx talks to the Taiwan TDX transport API and maps its v3 rail
// responses onto domain types.
//
// Everything format-specific is contained here: the OAuth2 dance, the request
// throttle the rate limit forces on us, the nested JSON shapes, and the naive
// "HH:mm" clock strings that have to be anchored to a service date before the
// domain can reason about them.
package tdx

// nameZhEn is TDX's bilingual name object, used for stations and train types.
type nameZhEn struct {
	ZhTw string `json:"Zh_tw"`
	En   string `json:"En"`
}

// v3 responses are always an object wrapping the array, never a bare array.

type odTimetableResponse struct {
	UpdateTime      string           `json:"UpdateTime"`
	TrainDate       string           `json:"TrainDate"`
	TrainTimetables []trainTimetable `json:"TrainTimetables"`
}

type trainTimetable struct {
	TrainInfo trainInfo  `json:"TrainInfo"`
	StopTimes []stopTime `json:"StopTimes"`
}

type trainInfo struct {
	TrainNo       string   `json:"TrainNo"`
	TrainTypeID   string   `json:"TrainTypeID"`
	TrainTypeCode string   `json:"TrainTypeCode"`
	TrainTypeName nameZhEn `json:"TrainTypeName"`
	TripHeadSign  string   `json:"TripHeadSign"`
	// SuspendedFlag on the train means the whole service is cancelled. The
	// same flag on a stop means this train skips that station. Both are
	// disqualifying, and both arrive with the timetable — so cancellation
	// detection needs no separate News call.
	SuspendedFlag int `json:"SuspendedFlag"`
}

type stopTime struct {
	StopSequence  int      `json:"StopSequence"`
	StationID     string   `json:"StationID"`
	StationName   nameZhEn `json:"StationName"`
	ArrivalTime   string   `json:"ArrivalTime"`
	DepartureTime string   `json:"DepartureTime"`
	SuspendedFlag int      `json:"SuspendedFlag"`
}

type liveBoardResponse struct {
	UpdateTime      string          `json:"UpdateTime"`
	TrainLiveBoards []trainLiveItem `json:"TrainLiveBoards"`
}

type trainLiveItem struct {
	TrainNo     string   `json:"TrainNo"`
	StationID   string   `json:"StationID"`
	StationName nameZhEn `json:"StationName"`
	// TrainStationStatus has only ever been observed as 2 and its meaning is
	// undocumented, so nothing here depends on it.
	TrainStationStatus int    `json:"TrainStationStatus"`
	DelayTime          int    `json:"DelayTime"`
	UpdateTime         string `json:"UpdateTime"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}
