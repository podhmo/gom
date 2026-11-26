package main

import (
	"encoding/json"
	"testing"
)

func TestEncodeShippableEventUsesObjectPayload(t *testing.T) {
	got := encodeEvent(Event_Shippable{V0: "order-1"})
	if !got.IsOk() {
		t.Fatalf("encodeEvent failed: %v", got.Error)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got.Value.ToSlice(), &decoded); err != nil {
		t.Fatalf("unmarshal encoded event: %v", err)
	}
	payload, ok := decoded["ShippableOrderPlaced"].(map[string]any)
	if !ok {
		t.Fatalf("ShippableOrderPlaced is not an object: %#v", decoded["ShippableOrderPlaced"])
	}
	if payload["orderId"] != "order-1" {
		t.Fatalf("orderId = %#v", payload["orderId"])
	}
}
