package config

import "testing"

func TestServerInitializationTracksOverlappingInstances(t *testing.T) {
	if IsServerInitializing() {
		t.Fatal("initialization state was not clean at test start")
	}

	BeginServerInitialization()
	t.Cleanup(EndServerInitialization)
	BeginServerInitialization()
	t.Cleanup(EndServerInitialization)
	if !IsServerInitializing() {
		t.Fatal("initialization should be active after two Begin calls")
	}

	EndServerInitialization()
	if !IsServerInitializing() {
		t.Fatal("first End cleared overlapping initialization")
	}

	EndServerInitialization()
	if IsServerInitializing() {
		t.Fatal("initialization remained active after final End")
	}
}

func TestIsServerInitializingHonorsLegacyMirrorAssignment(t *testing.T) {
	previous := INITSERVER
	t.Cleanup(func() { INITSERVER = previous })

	INITSERVER = true
	if !IsServerInitializing() {
		t.Fatal("legacy INITSERVER=true was not reflected")
	}
	INITSERVER = false
	if IsServerInitializing() {
		t.Fatal("legacy INITSERVER=false was not reflected")
	}
}
