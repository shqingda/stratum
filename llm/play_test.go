package llm

import (
	"context"
	"strings"
	"testing"
)

func TestPlaySummaryRequest(t *testing.T) {
	req := Request{
		System: "You are Stratum.",
		Messages: []Message{
			Text(User, "讲讲这个仓库"),
			Text(Assistant, "这是一个 Go 包。"),
			Text(User, "The conversation is about to outgrow the context window. Write a summary that lets you carry on."),
		},
	}
	reply, err := Collect((Play{}).Stream(context.Background(), req), nil)
	if err != nil {
		t.Fatalf("Play.Stream summary error: %v", err)
	}
	if strings.TrimSpace(reply.Message.PlainText()) == "" {
		t.Fatal("expected non-empty summary text from Play")
	}
}
