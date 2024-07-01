package target

import (
	"io"

	"golang.org/x/crypto/ssh"
)

// A target is something on which you can run a benchmark (usually a server, but could be e.g. a container).
type Target interface {
	// Runs the command as the root user and returns the combined output.
	RunCommand(cmd string) ([]byte, error)

	// Copies the local file to the remote, creating the remote path if it does not exist.
	CopyFileTo(localPath io.Reader, remotePath string) error

	// Copies the remote file to the local file.
	CopyFileFrom(remotePath string, localFile io.Writer) error

	// Opens an SSH connection as the root user.
	Client() (*ssh.Client, error)
}
