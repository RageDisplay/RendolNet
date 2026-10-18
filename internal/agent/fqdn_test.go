package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"rendolnet/internal/netcfg"
)

// ответ на www.example.com: CNAME edge.cdn.net, A 203.0.113.7
func dnsAnswer(t *testing.T, ttl uint32) []byte {
	t.Helper()
	q := dnsmessage.MustNewName("www.example.com.")
	cn := dnsmessage.MustNewName("edge.cdn.net.")
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 7, Response: true})
	_ = b.StartQuestions()
	_ = b.Question(dnsmessage.Question{Name: q, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	_ = b.StartAnswers()
	_ = b.CNAMEResource(dnsmessage.ResourceHeader{Name: q, Class: dnsmessage.ClassINET, TTL: ttl}, dnsmessage.CNAMEResource{CNAME: cn})
	_ = b.AResource(dnsmessage.ResourceHeader{Name: cn, Class: dnsmessage.ClassINET, TTL: ttl}, dnsmessage.AResource{A: [4]byte{203, 0, 113, 7}})
	out, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func fqdnConfig() netcfg.Config {
	c := cfg(2)
	c.Objects.Addresses = append(c.Objects.Addresses, netcfg.AddressObject{Name: "example", Domains: []string{"*.Example.com"}})
	c.Rules = append([]netcfg.Rule{{ID: "r-block", Name: "block", Enabled: true, From: []string{"lan"}, To: []string{"wan"},
		Destination: []string{"example"}, Action: netcfg.ActionDeny}}, c.Rules...)
	return c
}

func TestDomainObject(t *testing.T) {
	dp := &fakeDP{}
	a := New(slog.Default(), dp, &fakeNet{}, t.TempDir(), "test")
	c := fqdnConfig()
	if err := a.apply(context.Background(), &Applied{Config: c}); err != nil {
		t.Fatal(err)
	}
	plan := dp.applied[0]
	ids := plan.DomainSets["example.com"]
	if len(ids) != 1 || len(plan.Sets[ids[0]]) != 0 {
		t.Fatalf("набор домена: %v %v", plan.DomainSets, plan.Sets)
	}
	set := ids[0]

	a.ObserveDNS(dnsAnswer(t, 300))
	want := []string{fmt.Sprintf("+%d:203.0.113.7/32", set)}
	if !slices.Equal(dp.setOps, want) {
		t.Fatalf("операции: %v, ждали %v", dp.setOps, want)
	}
	a.ObserveDNS(dnsAnswer(t, 300)) // повтор не трогает карту
	if len(dp.setOps) != 1 {
		t.Fatalf("повтор: %v", dp.setOps)
	}

	// новое применение сохраняет выученный адрес в плане
	if err := a.apply(context.Background(), &Applied{Config: c}); err != nil {
		t.Fatal(err)
	}
	p2 := dp.applied[1]
	if !slices.Contains(p2.Sets[p2.DomainSets["example.com"][0]], netip.MustParsePrefix("203.0.113.7/32")) {
		t.Fatalf("после применения адреса нет: %v", p2.Sets)
	}

	// истечение убирает адрес
	a.fq.mu.Lock()
	ops := a.fq.expire(time.Now().Add(time.Hour))
	a.fq.mu.Unlock()
	if len(ops) != 1 || ops[0].pfx.String() != "203.0.113.7/32" {
		t.Fatalf("истечение: %+v", ops)
	}
	if info := a.fq.snapshot(); len(info) != 1 || len(info[0].Addresses) != 0 {
		t.Fatalf("снимок: %+v", info)
	}
}

func TestDomainNotMatched(t *testing.T) {
	dp := &fakeDP{}
	a := New(slog.Default(), dp, &fakeNet{}, t.TempDir(), "test")
	c := fqdnConfig()
	c.Objects.Addresses[len(c.Objects.Addresses)-1].Domains = []string{"ample.com"}
	if err := a.apply(context.Background(), &Applied{Config: c}); err != nil {
		t.Fatal(err)
	}
	a.ObserveDNS(dnsAnswer(t, 300))
	if len(dp.setOps) != 0 {
		t.Fatalf("ample.com совпал с example.com: %v", dp.setOps)
	}
}
