package docker

import (
	"archive/tar"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/wodby/wodby-cli/pkg/exec"
)

type passwdEntry struct {
	name string
	uid  uint32
	gid  uint32
}

type groupEntry struct {
	name string
	gid  uint32
}

// ResolveImageUserIdentity resolves an image USER value to the numeric
// identity Docker will use. Numeric identities work without account files;
// named identities are read from the stopped image container filesystem.
func (c *Client) ResolveImageUserIdentity(image, user string) (uint32, uint32, error) {
	if user == "" || user == "root" {
		return 0, 0, nil
	}
	if identity, ok := directNumericIdentity(user); ok {
		return identity.uid, identity.gid, nil
	}

	container, err := createAccountLookupContainer(image)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = exec.Command("docker", "rm", container).Run()
	}()

	passwd, passwdErr := copyContainerFile(container, "/etc/passwd")
	group, groupErr := copyContainerFile(container, "/etc/group")
	uid, gid, err := resolveImageUserIdentity(user, passwd, group)
	if err == nil {
		return uid, gid, nil
	}

	if passwdErr != nil || groupErr != nil {
		return 0, 0, errors.Wrapf(err, "failed to read account information from image %s", image)
	}
	return 0, 0, err
}

type numericIdentity struct {
	uid uint32
	gid uint32
}

func directNumericIdentity(user string) (numericIdentity, bool) {
	parts := strings.Split(user, ":")
	if len(parts) != 2 {
		return numericIdentity{}, false
	}
	uid, uidOK := parseID(parts[0])
	gid, gidOK := parseID(parts[1])
	return numericIdentity{uid: uid, gid: gid}, uidOK && gidOK
}

func createAccountLookupContainer(image string) (string, error) {
	output, err := exec.Command("docker", "create", "--entrypoint=", image, "/bin/true").CombinedOutput()
	if err != nil {
		return "", errors.Wrap(err, string(output))
	}
	container := strings.TrimSpace(string(output))
	if container == "" {
		return "", errors.New("docker create returned an empty container ID")
	}
	return container, nil
}

func copyContainerFile(container, filename string) ([]byte, error) {
	output, err := exec.Command("docker", "cp", fmt.Sprintf("%s:%s", container, filename), "-").CombinedOutput()
	if err != nil {
		return nil, errors.Wrap(err, string(output))
	}

	reader := tar.NewReader(bytes.NewReader(output))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read %s copied from image", filename)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read %s copied from image", filename)
		}
		return content, nil
	}

	return nil, errors.Errorf("%s copied from image did not contain a regular file", filename)
}

func resolveImageUserIdentity(user string, passwd, group []byte) (uint32, uint32, error) {
	if user == "" {
		return 0, 0, nil
	}
	parts := strings.Split(user, ":")
	if len(parts) > 2 || parts[0] == "" {
		return 0, 0, errors.Errorf("invalid image user %q", user)
	}

	passwdEntries := parsePasswd(passwd)
	uid, userIsNumeric := parseID(parts[0])
	var account *passwdEntry
	if userIsNumeric {
		account = findPasswdByUID(passwdEntries, uid)
	} else {
		account = findPasswdByName(passwdEntries, parts[0])
		if account == nil {
			if parts[0] == "root" {
				uid = 0
			} else {
				return 0, 0, errors.Errorf("image user %q was not found in /etc/passwd", parts[0])
			}
		} else {
			uid = account.uid
		}
	}

	if len(parts) == 1 {
		if account != nil {
			return uid, account.gid, nil
		}
		return uid, 0, nil
	}
	if parts[1] == "" {
		return 0, 0, errors.Errorf("invalid image user %q", user)
	}
	if gid, ok := parseID(parts[1]); ok {
		return uid, gid, nil
	}
	if parts[1] == "root" {
		return uid, 0, nil
	}
	entry := findGroupByName(parseGroup(group), parts[1])
	if entry == nil {
		return 0, 0, errors.Errorf("image group %q was not found in /etc/group", parts[1])
	}
	return uid, entry.gid, nil
}

func parsePasswd(content []byte) []passwdEntry {
	entries := []passwdEntry{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 4 {
			continue
		}
		uid, uidOK := parseID(fields[2])
		gid, gidOK := parseID(fields[3])
		if fields[0] == "" || !uidOK || !gidOK {
			continue
		}
		entries = append(entries, passwdEntry{name: fields[0], uid: uid, gid: gid})
	}
	return entries
}

func parseGroup(content []byte) []groupEntry {
	entries := []groupEntry{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 3 {
			continue
		}
		gid, ok := parseID(fields[2])
		if fields[0] == "" || !ok {
			continue
		}
		entries = append(entries, groupEntry{name: fields[0], gid: gid})
	}
	return entries
}

func findPasswdByName(entries []passwdEntry, name string) *passwdEntry {
	for i := range entries {
		if entries[i].name == name {
			return &entries[i]
		}
	}
	return nil
}

func findPasswdByUID(entries []passwdEntry, uid uint32) *passwdEntry {
	for i := range entries {
		if entries[i].uid == uid {
			return &entries[i]
		}
	}
	return nil
}

func findGroupByName(entries []groupEntry, name string) *groupEntry {
	for i := range entries {
		if entries[i].name == name {
			return &entries[i]
		}
	}
	return nil
}

func parseID(value string) (uint32, bool) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err == nil
}
