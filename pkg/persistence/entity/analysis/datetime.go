package analysis

import (
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
)

type DateLatitude struct {
	*entity.Model
	Year    int `json:"year"`
	Quarter int `json:"quarter"`
	Month   int `json:"month"`
	Week    int `json:"week"`
	Day     int `json:"day"`
}

func NewDateLatitude(date time.Time) *DateLatitude {
	dl := &DateLatitude{
		Model:   entity.NewModel(),
		Year:    date.Year(),
		Quarter: int(date.Month())/4 + 1,
		Month:   int(date.Month()),
		Week:    int(date.Weekday()),
		Day:     date.Day(),
	}
	dl.ID = uint(date.Unix())
	return dl
}
func (own *DateLatitude) GetHash() string {
	return utils.HashCodes(strconv.Itoa(own.Year), strconv.Itoa(own.Quarter), strconv.Itoa(own.Month), strconv.Itoa(own.Week), strconv.Itoa(own.Day))
}

type TimeLatitude struct {
	*entity.Model
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

func NewTimeLatitude(date time.Time) *TimeLatitude {
	tl := &TimeLatitude{
		Hour:   date.Hour(),
		Minute: date.Minute(),
	}
	return tl
}
