package target

import (
	"fmt"
	"io"
	"path"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SSHTarget struct {
	User    *string
	IP      *string
	SSHPort int
	Auths   []ssh.AuthMethod
}

func (t *SSHTarget) RunCommand(cmd string) ([]byte, error) {
	client, err := t.Client()
	if err != nil {
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	return session.CombinedOutput(cmd)
}

func (t *SSHTarget) CopyFileTo(localPath io.Reader, remotePath string) error {
	client, err := t.Client()
	if err != nil {
		return err
	}

	sftp, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sftp.Close()

	err = sftp.MkdirAll(path.Dir(remotePath))
	if err != nil {
		return err
	}

	dst, err := sftp.Create(remotePath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = dst.ReadFrom(localPath)
	return err
}

func (t *SSHTarget) CopyFileFrom(remotePath string, localFile io.Writer) error {
	client, err := t.Client()
	if err != nil {
		return err
	}

	sftp, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sftp.Close()

	dst, err := sftp.Open(remotePath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = dst.WriteTo(localFile)
	return err
}

func (t *SSHTarget) Client() (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            *t.User,
		Auth:            t.Auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	return ssh.Dial("tcp", fmt.Sprintf("%s:%d", *t.IP, t.SSHPort), cfg)
}
