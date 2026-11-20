package kiln

import (
	_ "embed"
	"fmt"
	"io"
	"strings"
)

// packageDoc is this package's doc, which kiln help prints. TestHelp holds it equal to the doc in kiln.go.
//
//go:embed help.txt
var packageDoc string

// allTopic is the topic of the whole package doc.
const allTopic = "all"

// Help writes the part of this package's doc that topic names: all of it, or the section under one heading. Topics
// lists each topic. Help answers ErrUsage for a topic the doc holds no section of.
func Help(out io.Writer, topic string) error {
	text, found := helpSections()[topic]
	if topic == allTopic {
		text, found = packageDoc, true
	}

	if !found {
		return fmt.Errorf("%w: kiln help has no topic %s, only %s", ErrUsage, topic, strings.Join(Topics(), ", "))
	}

	_, err := io.WriteString(out, strings.TrimRight(text, "\n")+"\n")
	return err
}

// Topics are the topics Help takes, in the doc's order: all, then each heading. A heading's topic is in lower case,
// with no leading "the", and a dash for each space. The gate's topic is gate.
func Topics() []string {
	topics := []string{allTopic}
	for line := range strings.Lines(packageDoc) {
		if heading, ok := strings.CutPrefix(line, "# "); ok {
			topics = append(topics, topicOf(heading))
		}
	}

	return topics
}

// helpSections are the sections of packageDoc by topic. Each starts with its heading.
func helpSections() map[string]string {
	sections := map[string]string{}
	topic := ""
	for line := range strings.Lines(packageDoc) {
		if heading, ok := strings.CutPrefix(line, "# "); ok {
			topic = topicOf(heading)
		}

		if topic != "" {
			sections[topic] += line
		}
	}

	return sections
}

// topicOf is the topic of the heading, as Topics says.
func topicOf(heading string) string {
	topic := strings.ToLower(strings.TrimSpace(heading))
	topic = strings.TrimPrefix(topic, "the ")
	return strings.ReplaceAll(topic, " ", "-")
}
