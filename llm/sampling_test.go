package llm

import "testing"

func TestSamplingReachesTheBody(t *testing.T) {
	temp, topP := 0.2, 0.9
	req := Request{Model: "m", Messages: []Message{Text(User, "hi")}, Temperature: &temp, TopP: &topP}
	if b := anthropicBody(req, anthropicOpts{}); b["temperature"] != temp || b["top_p"] != topP {
		t.Errorf("anthropic: %v %v", b["temperature"], b["top_p"])
	}
	thinking := req
	thinking.Effort = "high"
	if b := anthropicBody(thinking, anthropicOpts{}); b["temperature"] != nil || b["top_p"] != nil {
		t.Errorf("anthropic while thinking: %v %v", b["temperature"], b["top_p"])
	}
	if b := chatBody(req, ChatCompat{}); b["temperature"] != temp || b["top_p"] != topP {
		t.Errorf("chat: %v %v", b["temperature"], b["top_p"])
	}
	gen, _ := geminiBody(req)["generationConfig"].(map[string]any)
	if gen["temperature"] != temp || gen["topP"] != topP {
		t.Errorf("gemini: %v", gen)
	}
	if b := responsesBody(req); b["temperature"] != nil || b["top_p"] != nil {
		t.Errorf("responses: %v %v", b["temperature"], b["top_p"])
	}
	plain := Request{Model: "m", Messages: []Message{Text(User, "hi")}}
	if b := chatBody(plain, ChatCompat{}); b["temperature"] != nil || b["top_p"] != nil {
		t.Errorf("unset sampling sent: %v %v", b["temperature"], b["top_p"])
	}
}
