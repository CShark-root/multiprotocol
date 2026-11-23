package compile

import (
	"strconv"

	"panel/internal/panel/store"
)

const speedMarkBase = 0x50000000

func SpeedMark(userID int64) uint32 {
	if userID < 1 || userID > 0xffffff {
		return 0
	}
	return speedMarkBase | uint32(userID)
}

func speedTag(kind string, userID int64) string {
	return "speed-u" + strconv.FormatInt(userID, 10) + "-" + kind
}

func limitedUsers(users []store.User) []store.User {
	out := []store.User{}
	for _, u := range users {
		if u.EffectiveSpeedMbps() > 0 && SpeedMark(u.ID) != 0 {
			out = append(out, u)
		}
	}
	return out
}
