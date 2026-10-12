//go:build !wasm
// +build !wasm

package tests

import (
	"testing"
)

func TestBootstrap(t *testing.T) {}
func TestOnlineWrite(t *testing.T) {}
func TestOfflineWrite(t *testing.T) {}
func TestLostAnswer(t *testing.T) {}
func TestRejectedReverted(t *testing.T) {}
func TestRejectedUpdateRestores(t *testing.T) {}
func TestPull(t *testing.T) {}
func TestPullPagesFiltered(t *testing.T) {}
func TestPullWaitsForPush(t *testing.T) {}
func TestEpochChange(t *testing.T) {}
func TestSecondTab(t *testing.T) {}
func TestFirstRunOffline(t *testing.T) {}
func TestUnknownOp(t *testing.T) {}
func TestUnsupportedScopeOp(t *testing.T) {}
