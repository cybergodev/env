package env

// buildComponentFactory is a test-only convenience wrapper around the
// FileSystem-taking factory constructor: tests build factories without a
// custom file system, so they pass the process default implicitly. It lives
// here (rather than in factory.go) because no production code path calls it —
// New and UnmarshalMapWithConfig always go through buildComponentFactoryWithFS
// with an explicit FileSystem.
func (c *Config) buildComponentFactory() *ComponentFactory {
	return c.buildComponentFactoryWithFS(DefaultFileSystem)
}
