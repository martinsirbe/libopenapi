// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"sync"
)

// Schema represents a
type Schema struct {
	SchemaTypeRef        string
	Title                string
	MultipleOf           int64
	Maximum              int64
	ExclusiveMaximumBool bool
	ExclusiveMaximum     int64
	Minimum              int64
	ExclusiveMinimum     int64
	ExclusiveMinimumBool bool
	MaxLength            int64
	MinLength            int64
	Pattern              string
	Format               string
	MaxItems             int64
	MinItems             int64
	UniqueItems          int64
	MaxProperties        int64
	MinProperties        int64
	Required             []string
	Enum                 []string
	Type                 []string
	AllOf                []*SchemaProxy
	OneOf                []*SchemaProxy
	AnyOf                []*SchemaProxy
	Not                  []*SchemaProxy
	Items                []*SchemaProxy
	Properties           map[string]*SchemaProxy
	AdditionalProperties any
	Description          string
	Default              any
	Nullable             bool
	Discriminator        *Discriminator
	ReadOnly             bool
	WriteOnly            bool
	XML                  *XML
	ExternalDocs         *ExternalDoc
	Example              any
	Examples             []any
	Deprecated           bool
	Extensions           map[string]any
	low                  *base.Schema
}

func NewSchema(schema *base.Schema) *Schema {
	s := new(Schema)
	s.low = schema
	s.Title = schema.Title.Value
	s.MultipleOf = schema.MultipleOf.Value
	s.Maximum = schema.Maximum.Value
	s.Minimum = schema.Minimum.Value
	// if we're dealing with a 3.0 spec using a bool
	if !schema.ExclusiveMaximum.IsEmpty() && schema.ExclusiveMaximum.Value.IsA() {
		s.ExclusiveMaximumBool = schema.ExclusiveMaximum.Value.A
	}
	// if we're dealing with a 3.1 spec using an int
	if !schema.ExclusiveMaximum.IsEmpty() && schema.ExclusiveMaximum.Value.IsB() {
		s.ExclusiveMaximum = schema.ExclusiveMaximum.Value.B
	}
	// if we're dealing with a 3.0 spec using a bool
	if !schema.ExclusiveMinimum.IsEmpty() && schema.ExclusiveMinimum.Value.IsA() {
		s.ExclusiveMinimumBool = schema.ExclusiveMinimum.Value.A
	}
	// if we're dealing with a 3.1 spec, using an int
	if !schema.ExclusiveMinimum.IsEmpty() && schema.ExclusiveMinimum.Value.IsB() {
		s.ExclusiveMinimum = schema.ExclusiveMinimum.Value.B
	}
	s.MaxLength = schema.MaxLength.Value
	s.MinLength = schema.MinLength.Value
	s.Pattern = schema.Pattern.Value
	s.Format = schema.Format.Value
	s.MaxItems = schema.MaxItems.Value
	s.MinItems = schema.MinItems.Value
	s.MaxProperties = schema.MaxProperties.Value
	s.MinProperties = schema.MinProperties.Value

	// 3.0 spec is a single value
	if !schema.Type.IsEmpty() && schema.Type.Value.IsA() {
		s.Type = []string{schema.Type.Value.A}
	}
	// 3.1 spec may have multiple values
	if !schema.Type.IsEmpty() && schema.Type.Value.IsB() {
		for i := range schema.Type.Value.B {
			s.Type = append(s.Type, schema.Type.Value.B[i].Value)
		}
	}
	s.AdditionalProperties = schema.AdditionalProperties.Value
	s.Description = schema.Description.Value
	s.Default = schema.Default.Value
	s.Nullable = schema.Nullable.Value
	s.ReadOnly = schema.ReadOnly.Value
	s.WriteOnly = schema.WriteOnly.Value
	s.Example = schema.Example.Value
	s.Deprecated = schema.Deprecated.Value
	s.Extensions = high.ExtractExtensions(schema.Extensions)
	if !schema.Discriminator.IsEmpty() {
		s.Discriminator = NewDiscriminator(schema.Discriminator.Value)
	}
	if !schema.XML.IsEmpty() {
		s.XML = NewXML(schema.XML.Value)
	}
	if !schema.ExternalDocs.IsEmpty() {
		s.ExternalDocs = NewExternalDoc(schema.ExternalDocs.Value)
	}
	var req []string
	for i := range schema.Required.Value {
		req = append(req, schema.Required.Value[i].Value)
	}
	s.Required = req

	var enum []string
	for i := range schema.Enum.Value {
		enum = append(enum, schema.Enum.Value[i].Value)
	}
	s.Enum = enum

	// async work.
	// any polymorphic properties need to be handled in their own threads
	// any properties each need to be processed in their own thread.
	// we go as fast as we can.

	polyCompletedChan := make(chan bool)
	propsChan := make(chan bool)
	errChan := make(chan error)

	// schema async
	buildOutSchema := func(schemas []lowmodel.ValueReference[*base.SchemaProxy], items *[]*SchemaProxy,
		doneChan chan bool, e chan error) {
		bChan := make(chan *SchemaProxy)

		// for every item, build schema async
		buildSchemaChild := func(sch lowmodel.ValueReference[*base.SchemaProxy], bChan chan *SchemaProxy) {
			p := &SchemaProxy{schema: &lowmodel.NodeReference[*base.SchemaProxy]{
				ValueNode: sch.ValueNode,
				Value:     sch.Value,
			}}
			bChan <- p
		}
		totalSchemas := len(schemas)
		for v := range schemas {
			go buildSchemaChild(schemas[v], bChan)
		}
		j := 0
		for j < totalSchemas {
			select {
			case t := <-bChan:
				j++
				*items = append(*items, t)
			}
		}
		doneChan <- true
	}

	// props async
	plock := sync.RWMutex{}
	var buildProps = func(k lowmodel.KeyReference[string], v lowmodel.ValueReference[*base.SchemaProxy], c chan bool,
		props map[string]*SchemaProxy) {
		defer plock.Unlock()
		plock.Lock()
		props[k.Value] = &SchemaProxy{schema: &lowmodel.NodeReference[*base.SchemaProxy]{
			Value:     v.Value,
			KeyNode:   k.KeyNode,
			ValueNode: v.ValueNode,
		},
		}
		s.Properties = props
		c <- true
	}

	props := make(map[string]*SchemaProxy)
	for k, v := range schema.Properties.Value {
		go buildProps(k, v, propsChan, props)
	}

	var allOf []*SchemaProxy
	var oneOf []*SchemaProxy
	var anyOf []*SchemaProxy
	var not []*SchemaProxy
	var items []*SchemaProxy

	if !schema.AllOf.IsEmpty() {
		go buildOutSchema(schema.AllOf.Value, &allOf, polyCompletedChan, errChan)
	}
	if !schema.AnyOf.IsEmpty() {
		go buildOutSchema(schema.AnyOf.Value, &anyOf, polyCompletedChan, errChan)
	}
	if !schema.OneOf.IsEmpty() {
		go buildOutSchema(schema.OneOf.Value, &oneOf, polyCompletedChan, errChan)
	}
	if !schema.Not.IsEmpty() {
		go buildOutSchema(schema.Not.Value, &not, polyCompletedChan, errChan)
	}
	if !schema.Items.IsEmpty() {
		go buildOutSchema(schema.Items.Value, &items, polyCompletedChan, errChan)
	}

	completeChildren := 0
	completedProps := 0
	totalProps := len(schema.Properties.Value)
	totalChildren := len(schema.AllOf.Value) + len(schema.OneOf.Value) + len(schema.AnyOf.Value) + len(schema.Items.Value) + len(schema.Not.Value)
	if totalProps+totalChildren > 0 {
	allDone:
		for true {
			select {
			case <-polyCompletedChan:
				completeChildren++
				if totalProps == completedProps && totalChildren == completeChildren {
					break allDone
				}
			case <-propsChan:
				completedProps++
				if totalProps == completedProps && totalChildren == completeChildren {
					break allDone
				}
			}
		}
	}
	s.OneOf = oneOf
	s.AnyOf = anyOf
	s.AllOf = allOf
	s.Items = items
	s.Not = not

	return s
}

func (s *Schema) GoLow() *base.Schema {
	return s.low
}