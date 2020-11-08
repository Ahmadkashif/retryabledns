package dns

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const defaultPort = "53"

// Client is a DNS resolver client to resolve hostnames.
type Client struct {
	resolvers  []string
	maxRetries int
	rand       *rand.Rand
	mutex      *sync.Mutex
}

// Result contains the results from a DNS resolution
type Result struct {
	IPs []string
	TTL int
}

// New creates a new dns client
func New(baseResolvers []string, maxRetries int) *Client {
	client := Client{
		rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
		mutex:      &sync.Mutex{},
		maxRetries: maxRetries,
		resolvers:  baseResolvers,
	}
	return &client
}

// Resolve is the underlying resolve function that actually resolves a host
// and gets the ip records for that host.
func (c *Client) Resolve(host string) (Result, error) {
	msg := new(dns.Msg)

	msg.Id = dns.Id()
	msg.RecursionDesired = true
	msg.Question = make([]dns.Question, 1)
	msg.Question[0] = dns.Question{
		Name:   dns.Fqdn(host),
		Qtype:  dns.TypeA,
		Qclass: dns.ClassINET,
	}

	var err error
	var answer *dns.Msg

	result := Result{}

	for i := 0; i < c.maxRetries; i++ {
		c.mutex.Lock()
		resolver := c.resolvers[c.rand.Intn(len(c.resolvers))]
		c.mutex.Unlock()

		answer, err = dns.Exchange(msg, resolver)
		if err != nil {
			continue
		}

		// In case we got some error from the server, return.
		if answer != nil && answer.Rcode != dns.RcodeSuccess {
			return result, errors.New(dns.RcodeToString[answer.Rcode])
		}

		for _, record := range answer.Answer {
			// Add the IP and the TTL to the map
			if t, ok := record.(*dns.A); ok {
				result.IPs = append(result.IPs, t.A.String())
				result.TTL = int(t.Header().Ttl)
			}
		}
		return result, nil
	}

	return result, err
}

// ResolveRaw is the underlying resolve function that actually resolves a host
// and gets the raw records for that host.
func (c *Client) ResolveRaw(host string, requestType uint16) (results []string, raw string, err error) {
	msg := new(dns.Msg)

	msg.Id = dns.Id()
	msg.RecursionDesired = true
	msg.Question = make([]dns.Question, 1)
	msg.Question[0] = dns.Question{
		Name:   dns.Fqdn(host),
		Qtype:  requestType,
		Qclass: dns.ClassINET,
	}

	var answer *dns.Msg

	for i := 0; i < c.maxRetries; i++ {
		c.mutex.Lock()
		resolver := c.resolvers[c.rand.Intn(len(c.resolvers))]
		c.mutex.Unlock()

		answer, err = dns.Exchange(msg, resolver)
		if answer != nil {
			raw = answer.String()
		}
		if err != nil {
			continue
		}

		// In case we got some error from the server, return.
		if answer != nil && answer.Rcode != dns.RcodeSuccess {
			return results, raw, errors.New(dns.RcodeToString[answer.Rcode])
		}

		results = append(results, parse(answer, requestType)...)

		return results, raw, nil
	}

	return results, raw, err
}

// Do sends a provided dns request and return the raw native response
func (c *Client) Do(msg *dns.Msg) (resp *dns.Msg, err error) {

	for i := 0; i < c.maxRetries; i++ {
		resolver := c.resolvers[rand.Intn(len(c.resolvers))]
		resp, err = dns.Exchange(msg, resolver)
		if err != nil {
			continue
		}

		// In case we get a non empty answer stop retrying
		if resp != nil {
			return
		}
	}

	return
}

// ResolveEnrich sends a provided dns request and return enriched response
func (c *Client) ResolveEnrich(host string, requestType uint16) (*DNSData, error) {
	var (
		dnsdata DNSData
		err     error
		msg     dns.Msg
	)

	msg.Id = dns.Id()
	msg.RecursionDesired = true
	msg.Question = make([]dns.Question, 1)
	msg.Question[0] = dns.Question{
		Name:   dns.Fqdn(host),
		Qtype:  requestType,
		Qclass: dns.ClassINET,
	}

	for i := 0; i < c.maxRetries; i++ {
		resolver := c.resolvers[rand.Intn(len(c.resolvers))]
		var resp *dns.Msg
		resp, err = dns.Exchange(&msg, resolver)
		if err != nil {
			continue
		}

		dnsdata.Raw = resp.String()
		dnsdata.StatusCode = dns.RcodeToString[resp.Rcode]
		dnsdata.Resolver = resolver

		// In case we got some error from the server, return.
		if resp != nil && resp.Rcode != dns.RcodeSuccess {
			break
		}

		dnsdata.ParseFromMsg(resp)
		break
	}

	return &dnsdata, err
}

func parse(answer *dns.Msg, requestType uint16) (results []string) {
	for _, record := range answer.Answer {
		switch requestType {
		case dns.TypeA:
			if t, ok := record.(*dns.A); ok {
				results = append(results, t.String())
			}
		case dns.TypeNS:
			if t, ok := record.(*dns.NS); ok {
				results = append(results, t.String())
			}
		case dns.TypeCNAME:
			if t, ok := record.(*dns.CNAME); ok {
				results = append(results, t.String())
			}
		case dns.TypeSOA:
			if t, ok := record.(*dns.SOA); ok {
				results = append(results, t.String())
			}
		case dns.TypePTR:
			if t, ok := record.(*dns.PTR); ok {
				results = append(results, t.String())
			}
		case dns.TypeMX:
			if t, ok := record.(*dns.MX); ok {
				results = append(results, t.String())
			}
		case dns.TypeTXT:
			if t, ok := record.(*dns.TXT); ok {
				results = append(results, t.String())
			}
		case dns.TypeAAAA:
			if t, ok := record.(*dns.AAAA); ok {
				results = append(results, t.String())
			}
		}
	}

	return
}

type DNSData struct {
	Domain     string
	TTL        int
	Resolver   string
	A          []string
	AAAA       []string
	CNAME      []string
	MX         []string
	PTR        []string
	SOA        []string
	NS         []string
	TXT        []string
	Raw        string
	StatusCode string
}

// ParseFromMsg and enrich data
func (d *DNSData) ParseFromMsg(msg *dns.Msg) error {
	for _, record := range msg.Answer {
		switch record.(type) {
		case *dns.A:
			d.A = append(d.A, record.String())
		case *dns.NS:
			d.NS = append(d.NS, record.String())
		case *dns.CNAME:
			d.CNAME = append(d.CNAME, record.String())
		case *dns.SOA:
			d.SOA = append(d.SOA, record.String())
		case *dns.PTR:
			d.PTR = append(d.PTR, record.String())
		case *dns.MX:
			d.MX = append(d.MX, record.String())
		case *dns.TXT:
			d.TXT = append(d.TXT, record.String())
		case *dns.AAAA:
			d.AAAA = append(d.AAAA, record.String())
		}
	}

	return nil
}
