//go:build unix

package main

import "syscall"

// the database, backups and uploaded files are only readable by the account
// running the server (they hold password hashes, tokens and API keys)
func init() {
	syscall.Umask(0o077)
}
