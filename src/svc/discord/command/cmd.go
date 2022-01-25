package command

import (
	"errors"

	"github.com/bwmarrin/discordgo"
)

var (
	ErrCommandNotFound      = errors.New("command not found")
	ErrCommandAlreadyExists = errors.New("command already exists")
	ErrCommandDoesNotExist  = errors.New("command does not exist")
)

type Cmd interface {
	Name() string
	Match(path []string) bool
	Execute(s *discordgo.Session, m *discordgo.MessageCreate, path []string) error
}

type Command struct {
	NameCmd    func() string
	MatchCmd   func(path []string) bool
	ExecuteCmd func(s *discordgo.Session, m *discordgo.MessageCreate, path []string) error
}

func (c *Command) Name() string {
	return c.NameCmd()
}

func (c *Command) Match(path []string) bool {
	return c.MatchCmd(path)
}

func (c *Command) Execute(s *discordgo.Session, m *discordgo.MessageCreate, path []string) error {
	return c.ExecuteCmd(s, m, path)
}
