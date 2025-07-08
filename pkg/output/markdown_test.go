package output

import (
	"testing"

	"github.com/ohshell/cli/pkg/record"
	"github.com/stretchr/testify/suite"
)

// MarkdownTestSuite defines the test suite for the output/markdown package
type MarkdownTestSuite struct {
	suite.Suite
}

// SetupTest runs before each test
func (suite *MarkdownTestSuite) SetupTest() {
	// Setup test fixtures
}

// TearDownTest runs after each test
func (suite *MarkdownTestSuite) TearDownTest() {
	// Cleanup test fixtures
}

// TestMarkdownTestSuite runs the test suite
func TestMarkdownTestSuite(t *testing.T) {
	suite.Run(t, new(MarkdownTestSuite))
}

// TestFormatMarkdown tests the markdown formatting function
func (suite *MarkdownTestSuite) TestFormatMarkdown() {
	// TODO: Implement test for markdown formatting
	// This test should verify that markdown is formatted as expected
	suite.T().Skip("TODO: Implement markdown formatting test")
}

// TestToMarkdown_SimpleSession tests ToMarkdown with a simple session
func (suite *MarkdownTestSuite) TestToMarkdown_SimpleSession() {
	session := &record.Session{
		Commands: []record.Command{
			{
				Input:  "echo hello",
				Output: "hello\n",
			},
		},
	}
	md := ToMarkdown(session)
	suite.Contains(md, "### Step 1", "Should contain step header")
	suite.Contains(md, "**Command:**", "Should contain command label")
	suite.Contains(md, "echo hello", "Should contain command input")
	suite.Contains(md, "**Output:**", "Should contain output label")
	suite.Contains(md, "hello", "Should contain command output")
}

// TestToMarkdown_MultipleCommandsAndEdgeCases tests ToMarkdown with multiple commands, including no output, 'exit', and special characters
func (suite *MarkdownTestSuite) TestToMarkdown_MultipleCommandsAndEdgeCases() {
	session := &record.Session{
		Commands: []record.Command{
			{Input: "ls -la", Output: "file1\nfile2\n"},
			{Input: "echo $HOME", Output: "/home/user\n"},
			{Input: "exit", Output: ""},         // should be skipped
			{Input: "cat file.txt", Output: ""}, // no output
			{Input: "echo 'special: !@#'", Output: "!@#\n"},
		},
	}
	md := ToMarkdown(session)
	// Should not contain 'exit' command
	suite.NotContains(md, "exit", "Should not include 'exit' command")
	// Should contain all other commands
	suite.Contains(md, "ls -la")
	suite.Contains(md, "echo $HOME")
	suite.Contains(md, "cat file.txt")
	suite.Contains(md, "echo 'special: !@#'")
	// Should contain special characters
	suite.Contains(md, "!@#")
	// Should contain output for commands that have it
	suite.Contains(md, "file1\nfile2")
	suite.Contains(md, "/home/user")
	// Should not error on commands with no output
	suite.Contains(md, "cat file.txt")
}

// TestToMarkdown_EmptySession tests ToMarkdown with an empty session
func (suite *MarkdownTestSuite) TestToMarkdown_EmptySession() {
	session := &record.Session{}
	md := ToMarkdown(session)
	suite.Equal("", md, "Empty session should return empty markdown")
}

// Example of a simple unit test without the suite
func TestMarkdownBasicFunctionality(t *testing.T) {
	// TODO: Replace with actual test implementation
	t.Skip("TODO: Implement basic functionality test")
	// Example:
	// input := "# Title"
	// output := FormatMarkdown(input)
	// assert.Contains(t, output, "<h1>")
}
