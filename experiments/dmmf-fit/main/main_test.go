package main

import (
	"encoding/json"
	"testing"

	"github.com/podhmo/gom/stdlib/list"
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

func TestValidateOrderInputsUsesStdlibTraverseAndSequence(t *testing.T) {
	orders := list.List[UnvalidatedOrder]{
		{Id: "order-1", Lines: list.List[Line]{{Sku: "sku-1", Qty: 1}}},
		{Id: "order-2", Lines: list.List[Line]{{Sku: "sku-2", Qty: 2}}},
	}
	got := validateOrderInputs(orders)
	if !got.IsOk() {
		t.Fatalf("validateOrderInputs failed: %v", got.Error)
	}
	if len(got.Value) != 2 || got.Value[0].Id != "order-1" || got.Value[1].Id != "order-2" {
		t.Fatalf("validated orders = %#v", got.Value)
	}

	bad := validateOrderInputs(list.List[UnvalidatedOrder]{
		{Id: "bad", Lines: list.List[Line]{{Sku: "sku-0", Qty: 0}}},
	})
	if bad.IsOk() {
		t.Fatalf("invalid line was accepted: %#v", bad.Value)
	}
}

func TestShortCircuitLogicalOperators(t *testing.T) {
	if shortCircuitAnd(struct{}{}) {
		t.Fatal("false && rhs should be false without evaluating rhs")
	}
	if !shortCircuitOr(struct{}{}) {
		t.Fatal("true || rhs should be true without evaluating rhs")
	}
}
