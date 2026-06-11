package ai

import "context"

// DesignCategory is a catalog category the designer may reference from a
// product-grid block's source. Supplied by the caller (already org-scoped).
type DesignCategory struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// DesignRequest is a natural-language brief for a storefront page.
type DesignRequest struct {
	Prompt     string
	Categories []DesignCategory
}

// Block is a generated storefront block: {type, id, props}. It mirrors the
// shape the CMS persists and the @teggo/blocks renderer consumes — both the
// human builder and the AI emit this same structure.
type Block = map[string]any

// PageDesigner turns a brief into a block tree. ClaudePageDesigner uses the
// model; DeterministicPageDesigner produces a sensible template offline and is
// the default when no API key is configured (mirrors the chat Provider split).
type PageDesigner interface {
	Name() string
	Design(ctx context.Context, req DesignRequest) (blocks []Block, notes string, err error)
}

// blockTypes the designer is allowed to emit, kept in lockstep with the CMS
// validator (internal/modules/cms) and the frontend registry (@teggo/blocks).
// The CMS handler re-validates generated blocks, so this is a generation guide,
// not the security boundary.
const blockSchemaDoc = `Available block types (emit a JSON array of {"type","id","props"}):
- "hero":        props {"heading": string, "subheading": string, "cta": {"label","href"}|null}
- "rich-text":   props {"html": string}   // simple HTML: <p>, <h2>, <ul><li>, <strong>
- "banner":      props {"heading": string, "cta": {"label","href"}|null}
- "cta":         props {"heading": string, "cta": {"label","href"}}
- "product-grid":props {"heading": string, "source": {"kind":"category","category_id": number, "limit": number}}`
