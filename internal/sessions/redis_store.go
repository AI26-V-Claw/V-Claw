package sessions

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vclaw/internal/providers"
)

const (
	defaultRedisKeyPrefix   = "vclaw:session:"
	defaultRedisMaxMessages = 40
	defaultRedisDialTimeout = 5 * time.Second
)

type RedisStoreConfig struct {
	URL         string
	KeyPrefix   string
	MaxMessages int
	TTL         time.Duration
	DialTimeout time.Duration
}

type RedisStore struct {
	address     string
	username    string
	password    string
	db          string
	keyPrefix   string
	maxMessages int
	ttl         time.Duration
	dialTimeout time.Duration
}

func NewRedisStore(config RedisStoreConfig) (*RedisStore, error) {
	address, username, password, db, err := parseRedisURL(config.URL)
	if err != nil {
		return nil, err
	}
	keyPrefix := strings.TrimSpace(config.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = defaultRedisKeyPrefix
	}
	maxMessages := config.MaxMessages
	if maxMessages <= 0 {
		maxMessages = defaultRedisMaxMessages
	}
	dialTimeout := config.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = defaultRedisDialTimeout
	}
	return &RedisStore{
		address:     address,
		username:    username,
		password:    password,
		db:          db,
		keyPrefix:   keyPrefix,
		maxMessages: maxMessages,
		ttl:         config.TTL,
		dialTimeout: dialTimeout,
	}, nil
}

func (s *RedisStore) LoadTranscript(ctx context.Context, sessionID string) ([]providers.Message, error) {
	reply, err := s.execute(ctx, []string{"LRANGE", s.key(sessionID), "0", "-1"})
	if err != nil {
		return nil, err
	}
	items, ok := reply.([]any)
	if !ok {
		return nil, fmt.Errorf("redis LRANGE returned %T", reply)
	}
	messages := make([]providers.Message, 0, len(items))
	for _, item := range items {
		raw, ok := item.([]byte)
		if !ok {
			return nil, fmt.Errorf("redis transcript item has type %T", item)
		}
		var message providers.Message
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, fmt.Errorf("decode transcript item: %w", err)
		}
		messages = append(messages, message)
	}
	return cloneMessages(messages), nil
}

func (s *RedisStore) AppendMessage(ctx context.Context, sessionID string, message providers.Message) error {
	raw, err := json.Marshal(cloneMessage(message))
	if err != nil {
		return fmt.Errorf("encode transcript message: %w", err)
	}
	key := s.key(sessionID)
	commands := [][]string{
		{"RPUSH", key, string(raw)},
		{"LTRIM", key, strconv.Itoa(-s.maxMessages), "-1"},
	}
	if s.ttl > 0 {
		commands = append(commands, []string{"EXPIRE", key, strconv.Itoa(int(s.ttl.Seconds()))})
	}
	_, err = s.execute(ctx, commands...)
	return err
}

func (s *RedisStore) ClearSession(ctx context.Context, sessionID string) error {
	_, err := s.execute(ctx, []string{"DEL", s.key(sessionID)}, []string{"DEL", s.memoryKey(sessionID)})
	return err
}

func (s *RedisStore) LoadMemory(ctx context.Context, sessionID string) (SessionMemory, error) {
	reply, err := s.execute(ctx, []string{"GET", s.memoryKey(sessionID)})
	if err != nil {
		return SessionMemory{}, err
	}
	if reply == nil {
		return SessionMemory{}, nil
	}
	raw, ok := reply.([]byte)
	if !ok {
		return SessionMemory{}, fmt.Errorf("redis memory item has type %T", reply)
	}
	var memory SessionMemory
	if err := json.Unmarshal(raw, &memory); err != nil {
		return SessionMemory{}, fmt.Errorf("decode session memory: %w", err)
	}
	return cloneMemory(memory), nil
}

func (s *RedisStore) SaveMemory(ctx context.Context, sessionID string, memory SessionMemory) error {
	raw, err := json.Marshal(cloneMemory(memory))
	if err != nil {
		return fmt.Errorf("encode session memory: %w", err)
	}
	command := []string{"SET", s.memoryKey(sessionID), string(raw)}
	commands := [][]string{command}
	if s.ttl > 0 {
		commands = append(commands, []string{"EXPIRE", s.memoryKey(sessionID), strconv.Itoa(int(s.ttl.Seconds()))})
	}
	_, err = s.execute(ctx, commands...)
	return err
}

func (s *RedisStore) key(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	return s.keyPrefix + sessionID
}

func (s *RedisStore) memoryKey(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	return s.keyPrefix + "memory:" + sessionID
}

func (s *RedisStore) execute(ctx context.Context, commands ...[]string) (any, error) {
	if s == nil {
		return nil, fmt.Errorf("redis store is nil")
	}
	conn, err := (&net.Dialer{Timeout: s.dialTimeout}).DialContext(ctx, "tcp", s.address)
	if err != nil {
		return nil, fmt.Errorf("connect redis: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	reader := bufio.NewReader(conn)
	if s.password != "" {
		auth := []string{"AUTH", s.password}
		if s.username != "" {
			auth = []string{"AUTH", s.username, s.password}
		}
		if _, err := redisRoundTrip(conn, reader, auth); err != nil {
			return nil, err
		}
	}
	if s.db != "" {
		if _, err := redisRoundTrip(conn, reader, []string{"SELECT", s.db}); err != nil {
			return nil, err
		}
	}
	var reply any
	for _, command := range commands {
		reply, err = redisRoundTrip(conn, reader, command)
		if err != nil {
			return nil, err
		}
	}
	return reply, nil
}

func parseRedisURL(rawURL string) (address, username, password, db string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", "", "", "", fmt.Errorf("redis url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("parse redis url: %w", err)
	}
	if parsed.Scheme != "redis" && parsed.Scheme != "tcp" {
		return "", "", "", "", fmt.Errorf("redis url scheme must be redis or tcp")
	}
	host := parsed.Host
	if !strings.Contains(host, ":") {
		host += ":6379"
	}
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
		if password == "" && username != "" {
			password = username
			username = ""
		}
	}
	path := strings.Trim(parsed.Path, "/")
	if path != "" {
		if _, err := strconv.Atoi(path); err != nil {
			return "", "", "", "", fmt.Errorf("redis db must be numeric")
		}
		db = path
	}
	return host, username, password, db, nil
}

func redisRoundTrip(conn net.Conn, reader *bufio.Reader, command []string) (any, error) {
	if _, err := conn.Write([]byte(encodeRedisCommand(command))); err != nil {
		return nil, fmt.Errorf("write redis command %s: %w", redisCommandName(command), err)
	}
	reply, err := readRedisReply(reader)
	if err != nil {
		return nil, fmt.Errorf("read redis reply %s: %w", redisCommandName(command), err)
	}
	return reply, nil
}

func encodeRedisCommand(parts []string) string {
	var builder strings.Builder
	builder.WriteString("*")
	builder.WriteString(strconv.Itoa(len(parts)))
	builder.WriteString("\r\n")
	for _, part := range parts {
		builder.WriteString("$")
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteString("\r\n")
		builder.WriteString(part)
		builder.WriteString("\r\n")
	}
	return builder.String()
}

func redisCommandName(command []string) string {
	if len(command) == 0 {
		return "UNKNOWN"
	}
	return strings.ToUpper(strings.TrimSpace(command[0]))
}

func readRedisReply(reader *bufio.Reader) (any, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return readRedisLine(reader)
	case '-':
		line, err := readRedisLine(reader)
		if err != nil {
			return nil, err
		}
		return nil, errors.New(line)
	case ':':
		line, err := readRedisLine(reader)
		if err != nil {
			return nil, err
		}
		return strconv.ParseInt(line, 10, 64)
	case '$':
		line, err := readRedisLine(reader)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return nil, nil
		}
		data := make([]byte, size+2)
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}
		return data[:size], nil
	case '*':
		line, err := readRedisLine(reader)
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return nil, nil
		}
		items := make([]any, 0, size)
		for i := 0; i < size; i++ {
			item, err := readRedisReply(reader)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported redis reply prefix %q", prefix)
	}
}

func readRedisLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}
