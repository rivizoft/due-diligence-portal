package pdf

import (
	"io"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// readWatermarkContext follows api.ReadValidateAndOptimize, with a narrow
// compatibility adjustment for Content Credentials before validation.
func readWatermarkContext(in io.ReadSeeker, conf *model.Configuration) (*model.Context, error) {
	ctx, err := api.ReadContext(in, conf)
	if err != nil {
		return nil, err
	}
	if err := validateWatermarkContext(ctx); err != nil {
		return nil, err
	}
	if conf.Optimize {
		if err := api.OptimizeContext(ctx); err != nil {
			return nil, err
		}
	}
	if err := pdfcpu.CacheFormFonts(ctx); err != nil {
		return nil, err
	}
	return ctx, nil
}

func validateWatermarkContext(ctx *model.Context) error {
	// pdfcpu 0.11.1's AFRelationship allowlist predates C2PA_Manifest. Alias
	// this one known relationship only while validating; do not disable PDF
	// validation or change the associated-file metadata written to the copy.
	type originalRelationship struct {
		dict  types.Dict
		value types.Object
	}
	var patched []originalRelationship
	defer func() {
		for _, original := range patched {
			original.dict["AFRelationship"] = original.value
		}
	}()

	stack := make([]types.Object, 0, len(ctx.Table))
	for _, entry := range ctx.Table {
		if entry != nil && !entry.Free {
			stack = append(stack, entry.Object)
		}
	}
	for len(stack) > 0 {
		obj := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch v := obj.(type) {
		case types.Dict:
			if original, ok := v["AFRelationship"]; ok {
				value, err := ctx.Dereference(original)
				if err != nil {
					return err
				}
				if name, ok := value.(types.Name); ok && name == "C2PA_Manifest" {
					patched = append(patched, originalRelationship{dict: v, value: original})
					v["AFRelationship"] = types.Name("Unspecified")
				}
			}
			for _, child := range v {
				stack = append(stack, child)
			}
		case types.Array:
			stack = append(stack, v...)
		case types.StreamDict:
			stack = append(stack, v.Dict)
		}
	}
	return api.ValidateContext(ctx)
}
