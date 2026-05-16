package main

import (
	"strings"

	"github.com/mdp/qrterminal/v3"
	"rsc.io/qr"
)

// Half-block keeps the QR within 80×24 terminals.
func renderQR(data string) string {
	var sb strings.Builder
	qrterminal.GenerateHalfBlock(data, qr.M, &sb)
	return sb.String()
}
