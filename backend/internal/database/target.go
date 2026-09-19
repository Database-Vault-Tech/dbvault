package database

import "github.com/dbvault/dbvault/backend/internal/engine"

// Target and ServerInfo are engine-neutral; drivers in internal/engine/*
// know how to connect to each engine.
type (
	Target     = engine.Target
	ServerInfo = engine.ServerInfo
)
