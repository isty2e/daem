package configdocument

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/tailscale/hujson"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

func (document Document) commentsOutside(ctx context.Context, regions []RewriteRegion) ([]string, error) {
	if !document.exists {
		return nil, nil
	}
	if document.syntax == TOML {
		index, err := tomlsyntax.IndexDocument(ctx, document.content)
		if err != nil {
			return nil, err
		}
		var comments []string
		for _, comment := range index.ScopedComments() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			owned := false
			for _, region := range regions {
				if region.containsPropertyPath(comment.Path) {
					owned = true
					break
				}
			}
			if !owned {
				comments = append(comments, string(document.content[comment.Span.Start:comment.Span.End]))
			}
		}
		return comments, nil
	}
	lexical, err := scanJSON(ctx, document.content, document.syntax, document.limits.MaximumDepth)
	if err != nil {
		return nil, err
	}
	tree, err := parseJSONTree(lexical.normalized)
	if err != nil {
		return nil, err
	}
	var owned []sourceSpan
	for index, region := range regions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if regionCovered(regions, index) {
			continue
		}
		value := tree
		var start int
		present := true
		for _, segment := range region.path.Segments() {
			object, ok := value.Value.(*hujson.Object)
			if !ok {
				present = false
				break
			}
			present = false
			for _, member := range object.Members {
				var key string
				if err := json.Unmarshal(member.Name.Value.(hujson.Literal), &key); err != nil {
					return nil, err
				}
				if key == segment {
					value, start, present = member.Value, member.Name.StartOffset-len(member.Name.BeforeExtra), true
					break
				}
			}
			if !present {
				break
			}
		}
		if !present {
			continue
		}
		if region.kind == rewriteSubtree {
			owned = append(owned, sourceSpan{start, value.EndOffset + len(value.AfterExtra)})
		} else if array, ok := value.Value.(*hujson.Array); ok {
			for _, item := range region.indices {
				element := array.Elements[item]
				owned = append(owned, sourceSpan{element.StartOffset - len(element.BeforeExtra), element.EndOffset + len(element.AfterExtra)})
			}
		}
	}
	slices.SortFunc(owned, func(left, right sourceSpan) int { return left.start - right.start })
	var comments []string
	spanIndex, coveredEnd := 0, -1
	for index, comment := range lexical.comments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for spanIndex < len(owned) && owned[spanIndex].start <= lexical.spans[index].start {
			coveredEnd = max(coveredEnd, owned[spanIndex].end)
			spanIndex++
		}
		if coveredEnd < lexical.spans[index].end {
			comments = append(comments, comment)
		}
	}
	return comments, nil
}
