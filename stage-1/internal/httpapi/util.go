package httpapi

import "time"

func minutesToDuration(m int) time.Duration { return time.Duration(m) * time.Minute }

func nowUTC() time.Time { return time.Now().UTC() }
