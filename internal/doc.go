// Package internal implements the parsing, validation, and security
// machinery behind the root env package. It contains the .env line
// parser, the hand-written YAML lexer and parser, and the JSON flattener
// that turn source documents into flat key maps, together with the
// duplicate-key resolution policy shared by all three formats.
//
// It also provides the supporting services those parsers rely on:
// variable expansion with security limits, key and value validation,
// dot-notation path resolution, path validation for secure file access,
// secure io.Reader implementations, sensitive-key detection and masking,
// audit logging, struct marshaling and file I/O, key lookup, shared
// error types and dependency-injection interfaces, and resource pools.
package internal
