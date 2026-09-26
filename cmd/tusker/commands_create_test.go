package main

// Keep shared vault setup for tests of live commands.
func bootstrap(args Args) error { return bootstrapV7(args) }
