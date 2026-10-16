// rendolctl — командная строка к API RendolNet. Адрес и токен берутся из
// RN_URL и RN_TOKEN или флагов -url и -token.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

var version = "dev"

const usage = `rendolctl [-url URL] [-token TOKEN] [-k] команда

  status                    состояние узла и ревизии
  config get [running]      кандидат (или действующий конфиг) в JSON
  config set FILE           заменить кандидата содержимым файла
  config diff               отличия кандидата от действующего
  commit [-m TEXT] [-confirm SEC]
  confirm                   подтвердить ревизию, ждущую подтверждения
  revisions                 история
  sessions                  активные сессии
  events [-n N]             последние события
  version
`

type client struct {
	url, token string
	hc         *http.Client
}

func (c *client) do(method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, strings.TrimRight(c.url, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			Error  string `json:"error"`
			Errors []struct{ Path, Message string }
		}
		_ = json.Unmarshal(raw, &e)
		for _, fe := range e.Errors {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", fe.Path, fe.Message)
		}
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	if out != nil && len(raw) > 0 {
		if p, ok := out.(*json.RawMessage); ok {
			*p = raw
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	return nil
}

func main() {
	url := flag.String("url", os.Getenv("RN_URL"), "адрес rendol-api")
	token := flag.String("token", os.Getenv("RN_TOKEN"), "токен API")
	insecure := flag.Bool("k", os.Getenv("RN_INSECURE") != "", "не проверять сертификат")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if args[0] == "version" {
		fmt.Println(version)
		return
	}
	if *url == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "нужны RN_URL и RN_TOKEN (токен создаётся в разделе «Пользователи»)")
		os.Exit(2)
	}
	c := &client{url: *url, token: *token, hc: &http.Client{
		Timeout:   2 * time.Minute,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure}},
	}}
	if err := run(c, args); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func printJSON(raw json.RawMessage) {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		fmt.Println(buf.String())
	} else {
		fmt.Println(string(raw))
	}
}

func run(c *client, args []string) error {
	switch args[0] {
	case "status":
		var s struct {
			Version string `json:"version"`
			Agent   *struct {
				Revision  int64      `json:"revision"`
				Confirmed int64      `json:"confirmed_revision"`
				Deadline  *time.Time `json:"deadline"`
				XDP       string     `json:"xdp_mode"`
				LastError string     `json:"last_error"`
			} `json:"agent"`
		}
		if err := c.do("GET", "/v1/system", nil, &s); err != nil {
			return err
		}
		fmt.Println("версия     ", s.Version)
		if a := s.Agent; a != nil {
			fmt.Printf("ревизия     r%d (подтверждена r%d)\n", a.Revision, a.Confirmed)
			if a.Deadline != nil {
				fmt.Printf("ждёт подтверждения ещё %s\n", time.Until(*a.Deadline).Round(time.Second))
			}
			fmt.Println("XDP        ", a.XDP)
			if a.LastError != "" {
				fmt.Println("ошибка     ", a.LastError)
			}
		} else {
			fmt.Println("агент не отвечает")
		}
	case "config":
		if len(args) < 2 {
			return fmt.Errorf("config get|set|diff")
		}
		switch args[1] {
		case "get":
			path, field := "/v1/config/candidate", "body"
			if len(args) > 2 && args[2] == "running" {
				path = "/v1/config/running"
			}
			var m map[string]json.RawMessage
			if err := c.do("GET", path, nil, &m); err != nil {
				return err
			}
			printJSON(m[field])
		case "set":
			if len(args) < 3 {
				return fmt.Errorf("config set FILE")
			}
			b, err := os.ReadFile(args[2])
			if err != nil {
				return err
			}
			var body json.RawMessage = b
			var resp struct {
				Validation *struct {
					Errors []struct{ Path, Message string }
				} `json:"validation"`
			}
			if err := c.do("PUT", "/v1/config/candidate", map[string]any{"body": body}, &resp); err != nil {
				return err
			}
			if resp.Validation != nil {
				fmt.Println("кандидат сохранён, но не пройдёт проверку:")
				for _, e := range resp.Validation.Errors {
					fmt.Printf("  %s: %s\n", e.Path, e.Message)
				}
			} else {
				fmt.Println("кандидат сохранён")
			}
		case "diff":
			var d struct {
				Lines []struct{ Op, Text string } `json:"lines"`
			}
			if err := c.do("GET", "/v1/config/diff", nil, &d); err != nil {
				return err
			}
			for _, l := range d.Lines {
				if l.Op != " " {
					fmt.Println(l.Op + " " + l.Text)
				}
			}
		default:
			return fmt.Errorf("неизвестная подкоманда %s", args[1])
		}
	case "commit":
		fs := flag.NewFlagSet("commit", flag.ExitOnError)
		msg := fs.String("m", "", "комментарий")
		confirm := fs.Int("confirm", 0, "секунд на подтверждение")
		_ = fs.Parse(args[1:])
		var r struct {
			ID    int64  `json:"id"`
			State string `json:"state"`
		}
		if err := c.do("POST", "/v1/config/commit", map[string]any{"comment": *msg, "confirm_seconds": *confirm}, &r); err != nil {
			return err
		}
		fmt.Printf("r%d: %s\n", r.ID, r.State)
		if *confirm > 0 {
			fmt.Printf("подтвердите в течение %d с: rendolctl confirm\n", *confirm)
		}
	case "confirm":
		return c.do("POST", "/v1/config/confirm", map[string]any{}, nil)
	case "revisions":
		var r struct {
			Items []struct {
				ID      int64     `json:"id"`
				Created time.Time `json:"created_at"`
				Author  string    `json:"author"`
				Comment string    `json:"comment"`
				State   string    `json:"state"`
			} `json:"items"`
		}
		if err := c.do("GET", "/v1/config/revisions?limit=30", nil, &r); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "REV\tВРЕМЯ\tАВТОР\tСОСТОЯНИЕ\tКОММЕНТАРИЙ")
		for _, it := range r.Items {
			fmt.Fprintf(tw, "r%d\t%s\t%s\t%s\t%s\n", it.ID, it.Created.Local().Format("2006-01-02 15:04"), it.Author, it.State, it.Comment)
		}
		return tw.Flush()
	case "sessions":
		var raw json.RawMessage
		if err := c.do("GET", "/v1/sessions?limit=500", nil, &raw); err != nil {
			return err
		}
		printJSON(raw)
	case "events":
		fs := flag.NewFlagSet("events", flag.ExitOnError)
		n := fs.Int("n", 50, "сколько")
		_ = fs.Parse(args[1:])
		var evs []struct {
			At     time.Time `json:"at"`
			Action string    `json:"action"`
			Proto  int       `json:"proto"`
			Src    string    `json:"src"`
			Dst    string    `json:"dst"`
			Sport  int       `json:"sport"`
			Dport  int       `json:"dport"`
			Rule   string    `json:"rule_id"`
			Count  int       `json:"count"`
		}
		if err := c.do("GET", fmt.Sprintf("/v1/events?limit=%d", *n), nil, &evs); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, e := range evs {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s:%d\t%s:%d\t%s\t×%d\n", e.At.Local().Format("15:04:05"), e.Action, e.Proto, e.Src, e.Sport, e.Dst, e.Dport, e.Rule, e.Count)
		}
		return tw.Flush()
	default:
		return fmt.Errorf("неизвестная команда %s", args[0])
	}
	return nil
}
