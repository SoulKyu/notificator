package main

import (
	// Embed the IANA timezone database into the binary so time.LoadLocation
	// works in minimal runtimes (e.g. the alpine:latest container images, which
	// ship no /usr/share/zoneinfo). Without this, valid zones like
	// "Europe/Paris" fail to load: statistics handler validation rejects them
	// and backend bucketing silently falls back to UTC.
	_ "time/tzdata"

	"notificator/cmd"
)

func main() {
	cmd.Execute()
}
