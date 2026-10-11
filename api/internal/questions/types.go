package questions

import "time"

const (
	StatusOpen      = "open"
	StatusAnswered  = "answered"
	StatusWithdrawn = "withdrawn"
	StatusExpired   = "expired"

	maxRecordBytes = 16 * 1024
	maxDocBytes    = 800 * 1024
	maxBodyBytes   = 16 * 1024
	keepTerminal   = 300

	openPerRequester      = 3
	protectedPerRequester = 6
	openGlobal            = 20
	protectedGlobal       = 40
	hourlyGlobal          = 30
	hourlyPerRequester    = 6
	hourlyWindow          = time.Hour
	unreadKeep            = 7 * 24 * time.Hour
	inboxExpiredWindow    = 24 * time.Hour
	defaultTTLHours       = 24
	maxTTLHours           = 168
	maxDeliveries         = 4

	titleLimit     = 300
	bodyLimit      = 3000
	labelLimit     = 160
	detailLimit    = 300
	lineLimit      = 300
	threadLimit    = 120
	workLimit      = 40
	linkLimit      = 300
	linkTextLimit  = 160
	sessionLimit   = 128
	principalLimit = 64
	keyMin         = 16
	keyMax         = 64
)

// Question is one Owner question. IdempotencyKey and the two fingerprints are
// stored and never returned. Recommended is the option key, empty when none.
type Question struct {
	ID                 string      `json:"id"`
	Number             int         `json:"number"`
	Status             string      `json:"status"`
	Requester          string      `json:"requester"`
	RequesterPrincipal string      `json:"requester_principal"`
	RequesterThread    string      `json:"requester_thread,omitempty"`
	WorkID             string      `json:"work_id,omitempty"`
	Title              string      `json:"title"`
	Body               string      `json:"body,omitempty"`
	Options            []Option    `json:"options,omitempty"`
	Recommended        string      `json:"recommended,omitempty"`
	AllowText          bool        `json:"allow_text"`
	Urgency            string      `json:"urgency"`
	Meanwhile          string      `json:"meanwhile"`
	Link               string      `json:"link,omitempty"`
	LinkText           string      `json:"link_text,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	ExpiresAt          time.Time   `json:"expires_at"`
	IdempotencyKey     string      `json:"idempotency_key,omitempty"`
	RequestFP          string      `json:"request_fp,omitempty"`
	QuestionFP         string      `json:"question_fp,omitempty"`
	Answer             *Answer     `json:"answer,omitempty"`
	Withdrawal         *Withdrawal `json:"withdrawal,omitempty"`
	Deliveries         []Delivery  `json:"deliveries,omitempty"`
	Mine               bool        `json:"mine,omitempty"`
}

// Option is one server-lettered choice.
type Option struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// Answer is the Owner's reply. Approval is always false.
type Answer struct {
	Option   string       `json:"option,omitempty"`
	Text     string       `json:"text,omitempty"`
	By       string       `json:"by"`
	At       time.Time    `json:"at"`
	Channel  string       `json:"channel"`
	Approval bool         `json:"approval"`
	Read     *ReadReceipt `json:"read,omitempty"`
}

// ReadReceipt is the requester's own report that the thread used the answer.
type ReadReceipt struct {
	At        time.Time `json:"at"`
	Session   string    `json:"session"`
	Principal string    `json:"principal"`
}

// Withdrawal records who closed an open question, and why.
type Withdrawal struct {
	Session   string    `json:"session"`
	Principal string    `json:"principal"`
	At        time.Time `json:"at"`
	Reason    string    `json:"reason"`
}

// Delivery is one relay attempt. Values longer than the field cap are clipped.
type Delivery struct {
	Kind    string    `json:"kind,omitempty"`
	Channel string    `json:"channel,omitempty"`
	Target  string    `json:"target,omitempty"`
	At      time.Time `json:"at"`
	Result  string    `json:"result"`
	Error   string    `json:"error,omitempty"`
}

func (q Question) public(session string) Question {
	out := q.clone()
	out.IdempotencyKey = ""
	out.RequestFP = ""
	out.QuestionFP = ""
	out.Mine = session != "" && session == q.Requester
	return out
}

func (q Question) clone() Question {
	if q.Options != nil {
		q.Options = append([]Option(nil), q.Options...)
	}
	if q.Deliveries != nil {
		q.Deliveries = append([]Delivery(nil), q.Deliveries...)
	}
	if q.Answer != nil {
		a := *q.Answer
		if a.Read != nil {
			r := *a.Read
			a.Read = &r
		}
		q.Answer = &a
	}
	if q.Withdrawal != nil {
		w := *q.Withdrawal
		q.Withdrawal = &w
	}
	return q
}

func (q Question) protected(now time.Time) bool {
	if q.Status == StatusOpen {
		return true
	}
	return q.unread() && now.Before(q.Answer.At.Add(unreadKeep))
}

func (q Question) unread() bool {
	return q.Status == StatusAnswered && q.Answer != nil && q.Answer.Read == nil
}

func (q Question) terminalAt() time.Time {
	switch q.Status {
	case StatusAnswered:
		if q.Answer != nil {
			return q.Answer.At
		}
	case StatusWithdrawn:
		if q.Withdrawal != nil {
			return q.Withdrawal.At
		}
	case StatusExpired:
		return q.ExpiresAt
	}
	return q.CreatedAt
}

func (q Question) ordinary(now time.Time) bool {
	switch q.Status {
	case StatusOpen:
		return false
	case StatusAnswered, StatusWithdrawn, StatusExpired:
		return !q.protected(now)
	default:
		return false
	}
}

func numberLabel(n int) string {
	return "Q" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
