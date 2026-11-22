package session

import (
	"context"
	"testing"
	"time"

	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func TestPromptImages(t *testing.T) {
	script := &llm.Script{Turns: []llm.Turn{{Text: "a cat"}}}
	m, s := changesManager(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := make(chan struct{}, 1)
	defer m.Subscribe(func(e proto.Event) {
		if e.Type == proto.EventTurnEnd {
			ended <- struct{}{}
		}
	})()
	img := llm.Image{MediaType: "image/png", Data: []byte("png")}
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Images: []llm.Image{img}}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("the turn did not end")
	}
	msgs := script.Requests[0].Messages
	last := msgs[len(msgs)-1]
	if len(last.Parts) != 1 || last.Parts[0].Image == nil || string(last.Parts[0].Image.Data) != "png" {
		t.Fatalf("%+v", last)
	}
	h, _ := m.history(ctx, s.ID)
	if len(h) == 0 || h[0].Parts[0].Image == nil {
		t.Fatalf("history lost the image: %+v", h)
	}
}
