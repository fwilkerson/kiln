package gittest

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// author makes each commit at the epoch, so a commit's ID follows from its tree and parent alone.
const author = "gittest <gittest@example.com> 0 +0000"

// message is each commit's message.
const message = "gittest\n"

// emptyTree is git's ID of the tree that holds no file. git reads it as a Rev, where the stand-in reads only commits.
var emptyTree = git.Rev(object("tree", ""))

// object is git's ID of an object of kind, such as blob, whose content is body.
func object(kind, body string) string {
	sum := sha1.Sum(fmt.Appendf(nil, "%s %d\x00%s", kind, len(body), body))
	return hex.EncodeToString(sum[:])
}

// treeID is git's ID of the directory dir in files, where "" is the top.
func treeID(files map[string]blob, dir string) string {
	var body strings.Builder
	for _, c := range children(files, dir) {
		mode, id := c.mode, object("blob", c.content)
		if c.dir {
			mode, id = "40000", treeID(files, c.path)
		}

		raw, _ := hex.DecodeString(id)
		body.WriteString(mode + " " + c.name + "\x00" + string(raw))
	}

	return object("tree", body.String())
}

// commitID is git's ID of a commit of tree, on parent, or on none when parent is "".
func commitID(tree string, parent git.Rev) git.Rev {
	body := "tree " + tree + "\n"
	if parent != "" {
		body += "parent " + string(parent) + "\n"
	}

	return git.Rev(object("commit", body+"author "+author+"\ncommitter "+author+"\n\n"+message))
}
