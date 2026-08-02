package main

import (
	"errors"
	"fmt"
)

// apiResponse is the wrapper GLKVM returns for every API call.
type apiResponse[T any] struct {
	OK     bool   `json:"ok"`
	Result T      `json:"result"`
	Error  string `json:"error"`
}

// httpStatusError is returned when GLKVM responds with a non-2xx status.
type httpStatusError struct {
	statusCode int
	body       string
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("GLKVM returned HTTP %d: %s", e.statusCode, e.body)
}

// apiError converts an ok=false API response into an error.
func apiError(message string) error {
	if message == "" {
		return errors.New("GLKVM API returned ok=false")
	}
	return fmt.Errorf("GLKVM API returned ok=false: %s", message)
}

// atxState is the power/HDD LED state reported by GET /api/atx.
type atxState struct {
	Busy    bool   `json:"busy"`
	Enabled bool   `json:"enabled"`
	Power   string `json:"power"`
	LEDs    struct {
		Power bool `json:"power"`
		HDD   bool `json:"hdd"`
	} `json:"leds"`
}

func (s atxState) isPoweredOn() bool {
	return s.Power == "on" || s.Power == "sleep"
}

func printState(state atxState) {
	fmt.Printf("enabled:  %t\n", state.Enabled)
	fmt.Printf("busy:     %t\n", state.Busy)
	fmt.Printf("power:    %s\n", state.Power)
	fmt.Printf("power LED: %s\n", onOff(state.LEDs.Power))
	fmt.Printf("hdd LED:   %s\n", onOff(state.LEDs.HDD))
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}
