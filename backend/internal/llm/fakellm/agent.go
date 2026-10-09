package fakellm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// Tool names the agent heuristics know. They mirror the agents package constants (a test keeps
// them in sync) without importing it.
const (
	toolCreateOrderRequest = "create_order_request"
	toolGetRequestStatus   = "get_request_status"
	toolListAddresses      = "list_addresses"
	toolConfirmOrder       = "confirm_order"
	toolCancelRequest      = "cancel_request"
)

var (
	reUUID      = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reCancel    = regexp.MustCompile(`(?i)\b(cancel|abort|scrap|never ?mind)\b`)
	reStatus    = regexp.MustCompile(`(?i)\b(status|where is|where's|update on|progress|how is|how's|any news|check on)\b`)
	reAddresses = regexp.MustCompile(`(?i)\b(address(es)?|deliver(y)? (to|location)|where (can|do) you deliver|locations)\b`)
	reConfirm   = regexp.MustCompile(`(?i)\b(yes|yep|yeah|confirm(ed)?|go ahead|place (it|the order)|approve it|do it|sounds good|proceed|ok(ay)?)\b`)
	reOrderish  = regexp.MustCompile(`(?i)\b(need|order|buy|get|restock|reorder|running (low|out)|out of|purchase|the usual|want|send)\b|\d`)
	reNewItems  = regexp.MustCompile(`(?i)\b\d+\s+(?:x\s+)?[a-z]`)
	reDefault   = regexp.MustCompile(`(?i)\b(default|usual (address|place)|same (address|place))\b`)
)

func hasTool(req llm.ChatRequest, name string) bool {
	for _, t := range req.Tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

type toolEvent struct {
	name   string
	args   json.RawMessage
	result string
}

// history pairs every assistant tool call with its tool result, oldest first.
func history(req llm.ChatRequest) []toolEvent {
	calls := map[string]*toolEvent{}
	var out []*toolEvent
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleAssistant:
			for _, tc := range m.ToolCalls {
				ev := &toolEvent{name: tc.Name, args: tc.Arguments}
				calls[tc.ID] = ev
				out = append(out, ev)
			}
		case llm.RoleTool:
			if ev, ok := calls[m.ToolCallID]; ok {
				ev.result = m.Content
			} else {
				out = append(out, &toolEvent{name: m.Name, result: m.Content})
			}
		}
	}
	res := make([]toolEvent, len(out))
	for i, e := range out {
		res[i] = *e
	}
	return res
}

type address struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	IsDefault bool   `json:"is_default"`
	Line1     string `json:"address_line1"`
	Postal    string `json:"postal_code"`
}

// parseAddresses accepts [..], {"addresses": [..]} or {"items": [..]}.
func parseAddresses(result string) []address {
	var list []address
	if json.Unmarshal([]byte(result), &list) == nil && len(list) > 0 {
		return list
	}
	var wrap struct {
		Addresses []address `json:"addresses"`
		Items     []address `json:"items"`
	}
	if json.Unmarshal([]byte(result), &wrap) == nil {
		if len(wrap.Addresses) > 0 {
			return wrap.Addresses
		}
		return wrap.Items
	}
	return nil
}

// requestInfo pulls request id / status / total from a tool result of unknown exact shape.
type requestInfo struct {
	ID, Status string
	TotalCents int64
	HasTotal   bool
	Error      string
}

func parseRequestInfo(result string) requestInfo {
	var m map[string]any
	if json.Unmarshal([]byte(result), &m) != nil {
		return requestInfo{}
	}
	var ri requestInfo
	if e, ok := m["error"]; ok {
		switch v := e.(type) {
		case string:
			ri.Error = v
		case map[string]any:
			if s, ok := v["message"].(string); ok {
				ri.Error = s
			} else if s, ok := v["code"].(string); ok {
				ri.Error = s
			}
		}
	}
	look := []map[string]any{m}
	if r, ok := m["request"].(map[string]any); ok {
		look = append([]map[string]any{r}, look...)
	}
	for _, mm := range look {
		if ri.ID == "" {
			for _, k := range []string{"request_id", "id"} {
				if s, ok := mm[k].(string); ok && s != "" {
					ri.ID = s
					break
				}
			}
		}
		if ri.Status == "" {
			if s, ok := mm["status"].(string); ok {
				ri.Status = s
			}
		}
		if !ri.HasTotal {
			for _, k := range []string{"total_cents", "approved_total_cents", "estimated_total_cents"} {
				if f, ok := mm[k].(float64); ok {
					ri.TotalCents, ri.HasTotal = int64(f), true
					break
				}
			}
		}
	}
	return ri
}

// latestRequestID finds the most recent request id mentioned in the conversation.
func latestRequestID(req llm.ChatRequest, evs []toolEvent) string {
	if u, ok := lastUser(req); ok {
		if id := reUUID.FindString(u.Content); id != "" {
			return id
		}
	}
	for i := len(evs) - 1; i >= 0; i-- {
		var a struct {
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(evs[i].args, &a) == nil && a.RequestID != "" {
			return a.RequestID
		}
		if evs[i].name == toolCreateOrderRequest || evs[i].name == toolGetRequestStatus {
			if ri := parseRequestInfo(evs[i].result); ri.ID != "" {
				return ri.ID
			}
		}
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if id := reUUID.FindString(req.Messages[i].Content); id != "" {
			return id
		}
	}
	return ""
}

func latestAddresses(evs []toolEvent) []address {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].name == toolListAddresses {
			if a := parseAddresses(evs[i].result); len(a) > 0 {
				return a
			}
		}
	}
	return nil
}

// pickAddress matches the user's words against address labels, ids, postal codes or "default".
func pickAddress(text string, addrs []address) (address, bool) {
	lt := strings.ToLower(text)
	nt := llm.NormalizeText(text)
	for _, a := range addrs {
		if a.ID != "" && strings.Contains(lt, strings.ToLower(a.ID)) {
			return a, true
		}
	}
	best, bestLen := address{}, 0
	for _, a := range addrs {
		nl := llm.NormalizeText(a.Label)
		if nl != "" && ContainsPhraseOrEqual(nt, nl) && len(nl) > bestLen {
			best, bestLen = a, len(nl)
		}
		if a.Postal != "" && strings.Contains(lt, a.Postal) && bestLen == 0 {
			best, bestLen = a, 1
		}
	}
	if bestLen > 0 {
		return best, true
	}
	// Partial label word match ("marina" for "Marina Bay HQ").
	for _, a := range addrs {
		for _, w := range significant(llm.NormalizeText(a.Label)) {
			if len(w) >= 4 && llm.ContainsPhrase(nt, w) {
				return a, true
			}
		}
	}
	if reDefault.MatchString(text) {
		for _, a := range addrs {
			if a.IsDefault {
				return a, true
			}
		}
	}
	return address{}, false
}

func (f *Fake) call(name string, args any) llm.Message {
	b, _ := json.Marshal(args)
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: f.nextCallID(), Name: name, Arguments: b}}}
}

func text(s string) llm.Message { return llm.Message{Role: llm.RoleAssistant, Content: s} }

// answerAgent picks the next tool call or reply for a tool-enabled conversation.
func (f *Fake) answerAgent(req llm.ChatRequest) llm.Message {
	msgs := req.Messages
	if len(msgs) == 0 {
		return text("How can I help with office supplies today?")
	}
	evs := history(req)
	last := msgs[len(msgs)-1]
	if last.Role == llm.RoleTool {
		return f.afterTool(req, evs)
	}
	u, ok := lastUser(req)
	if !ok {
		return text("How can I help with office supplies today?")
	}
	ut := u.Content
	reqID := latestRequestID(req, evs)

	if reqID != "" && hasTool(req, toolConfirmOrder) {
		addrs := latestAddresses(evs)
		picked, okPick := pickAddress(ut, addrs)
		wantsConfirm := reConfirm.MatchString(ut) && !reNewItems.MatchString(ut)
		switch {
		case okPick && (wantsConfirm || !reNewItems.MatchString(ut)) && !reCancel.MatchString(ut) && !reStatus.MatchString(ut):
			return f.call(toolConfirmOrder, map[string]string{"request_id": reqID, "address_id": picked.ID})
		case wantsConfirm && len(addrs) == 0 && hasTool(req, toolListAddresses):
			return f.call(toolListAddresses, map[string]any{})
		case wantsConfirm && len(addrs) == 0:
			return text("Which delivery address should I use?")
		case wantsConfirm:
			return text("Which delivery address should I use? " + addressChoices(addrs))
		}
	}

	switch {
	case reCancel.MatchString(ut) && hasTool(req, toolCancelRequest):
		if reqID == "" {
			return text("Which request should I cancel?")
		}
		return f.call(toolCancelRequest, map[string]string{"request_id": reqID})

	case reStatus.MatchString(ut) && hasTool(req, toolGetRequestStatus):
		if reqID == "" {
			return text("Which request would you like me to check?")
		}
		return f.call(toolGetRequestStatus, map[string]string{"request_id": reqID})

	case reAddresses.MatchString(ut) && hasTool(req, toolListAddresses):
		return f.call(toolListAddresses, map[string]any{})

	case reOrderish.MatchString(ut) && hasTool(req, toolCreateOrderRequest):
		return f.call(toolCreateOrderRequest, map[string]string{"utterance": strings.TrimSpace(ut)})
	}
	return text("I can order office supplies for you. Tell me what you need, for example \"10 reams of A4 paper\".")
}

func addressChoices(addrs []address) string {
	var parts []string
	for _, a := range addrs {
		p := a.Label
		if a.IsDefault {
			p += " (default)"
		}
		parts = append(parts, p)
	}
	return "Options: " + strings.Join(parts, ", ") + "."
}

// afterTool phrases a reply from the latest tool result.
func (f *Fake) afterTool(req llm.ChatRequest, evs []toolEvent) llm.Message {
	if len(evs) == 0 {
		return text("Done.")
	}
	ev := evs[len(evs)-1]
	ri := parseRequestInfo(ev.result)
	if ri.Error != "" {
		return text("Sorry, that did not work: " + ri.Error + ". Shall I try again?")
	}
	switch ev.name {
	case toolCreateOrderRequest:
		// Never read IDs to the user (the real system prompt says the same).
		return text("I've started your request and I'm checking prices with our approved vendors. " +
			"Once the prices are in I'll read the items back and ask which delivery address to use.")
	case toolGetRequestStatus:
		s := fmt.Sprintf("Your request is %s.", statusWords(ri.Status))
		if ri.HasTotal && ri.TotalCents > 0 {
			s += fmt.Sprintf(" Total S$%d.%02d.", ri.TotalCents/100, ri.TotalCents%100)
		}
		if ri.Status == "quoted" {
			s += " Shall I confirm it? Which delivery address should I use?"
		}
		return text(s)
	case toolListAddresses:
		addrs := parseAddresses(ev.result)
		if len(addrs) == 0 {
			return text("There are no saved delivery addresses yet. Please ask an admin to add one.")
		}
		return text("Which delivery address should I use? " + addressChoices(addrs))
	case toolConfirmOrder:
		if ri.Status != "" {
			return text(fmt.Sprintf("Confirmed. The request is now %s.", statusWords(ri.Status)))
		}
		return text("Confirmed. I'll keep you posted on approval and payment.")
	case toolCancelRequest:
		return text("The request is cancelled.")
	}
	return text("Done.")
}

func statusWords(s string) string {
	switch s {
	case "":
		return "in progress"
	case "pending_approval":
		return "waiting for an approver"
	case "awaiting_payment":
		return "waiting for the payment to be approved"
	case "checking_out":
		return "checking out"
	}
	return strings.ReplaceAll(s, "_", " ")
}
