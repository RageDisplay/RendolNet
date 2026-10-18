package netcfg

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

type Resolver struct {
	addrs    map[string][]string
	domains  map[string][]string
	groups   map[string][]string
	services map[string]Service
}

func NewResolver(c *Config) *Resolver {
	r := &Resolver{
		addrs:    map[string][]string{},
		domains:  map[string][]string{},
		groups:   map[string][]string{},
		services: map[string]Service{},
	}
	for _, a := range c.Objects.Addresses {
		r.addrs[a.Name] = a.Prefixes
		for _, d := range a.Domains {
			r.domains[a.Name] = append(r.domains[a.Name], NormalizeDomain(d))
		}
	}
	for _, g := range c.Objects.Groups {
		r.groups[g.Name] = g.Members
	}
	for _, s := range c.Objects.Services {
		r.services[s.Name] = s
	}
	return r
}

// Addresses раскрывает список имён и литералов в набор сетей. nil означает
// «любой адрес».
func (r *Resolver) Addresses(refs []string) ([]netip.Prefix, error) {
	if isAny(refs) {
		return nil, nil
	}
	set := map[netip.Prefix]struct{}{}
	for _, ref := range refs {
		if err := r.expand(ref, set, map[string]bool{}); err != nil {
			return nil, err
		}
	}
	out := make([]netip.Prefix, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if c := out[i].Addr().Compare(out[j].Addr()); c != 0 {
			return c < 0
		}
		return out[i].Bits() < out[j].Bits()
	})
	return out, nil
}

func (r *Resolver) expand(ref string, set map[netip.Prefix]struct{}, seen map[string]bool) error {
	ref = strings.TrimSpace(ref)
	if ps, ok := r.addrs[ref]; ok {
		for _, s := range ps {
			p, err := ParseAddrOrPrefix(s)
			if err != nil {
				return err
			}
			set[p] = struct{}{}
		}
		return nil
	}
	if members, ok := r.groups[ref]; ok {
		if seen[ref] {
			return fmt.Errorf("группа %q включает саму себя", ref)
		}
		seen[ref] = true
		for _, m := range members {
			if err := r.expand(m, set, seen); err != nil {
				return err
			}
		}
		delete(seen, ref)
		return nil
	}
	p, err := ParseAddrOrPrefix(ref)
	if err != nil {
		return fmt.Errorf("%q: не объект и не адрес", ref)
	}
	set[p] = struct{}{}
	return nil
}

// NormalizeDomain: «*.Example.com.» и «example.com» — одно и то же,
// поддомены входят всегда.
func NormalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "*.")
	return strings.TrimSuffix(d, ".")
}

// Domains — домены объектов из списка, с раскрытием групп.
func (r *Resolver) Domains(refs []string) []string {
	if isAny(refs) {
		return nil
	}
	set := map[string]bool{}
	var walk func(ref string, seen map[string]bool)
	walk = func(ref string, seen map[string]bool) {
		ref = strings.TrimSpace(ref)
		for _, d := range r.domains[ref] {
			set[d] = true
		}
		if members, ok := r.groups[ref]; ok && !seen[ref] {
			seen[ref] = true
			for _, m := range members {
				walk(m, seen)
			}
		}
	}
	for _, ref := range refs {
		walk(ref, map[string]bool{})
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Services раскрывает имена сервисов и литералы ("tcp/443", "udp/53-54",
// "icmp"). nil — любой трафик.
func (r *Resolver) Services(refs []string) ([]ServiceSpec, error) {
	if isAny(refs) {
		return nil, nil
	}
	var out []ServiceSpec
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if s, ok := r.services[ref]; ok {
			spec, err := ParseService(s.Proto, s.Ports)
			if err != nil {
				return nil, fmt.Errorf("сервис %q: %w", ref, err)
			}
			out = append(out, spec)
			continue
		}
		proto, ports, _ := strings.Cut(ref, "/")
		spec, err := ParseService(Proto(strings.ToLower(proto)), ports)
		if err != nil {
			return nil, fmt.Errorf("%q: не сервис и не литерал вида tcp/443", ref)
		}
		out = append(out, spec)
	}
	return out, nil
}

func isAny(refs []string) bool {
	if len(refs) == 0 {
		return true
	}
	for _, r := range refs {
		if strings.EqualFold(strings.TrimSpace(r), "any") {
			return true
		}
	}
	return false
}
