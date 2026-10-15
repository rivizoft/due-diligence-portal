package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/stretchr/testify/require"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

// Build a synthetic PDF with the same associated-file relationship that causes
// older pdfcpu releases to reject Content Credentials. No customer data is used.
func c2paFixture(t *testing.T, relationship string, direct, brokenPages, indirectRelationship bool) []byte {
	t.Helper()
	content := "BT /F1 18 Tf 40 150 Td (Synthetic C2PA document) Tj ET\n"
	relationshipValue := "/" + relationship
	if indirectRelationship {
		relationshipValue = "9 0 R"
	}
	spec := fmt.Sprintf("<< /Type /Filespec /F (manifest.c2pa) /UF (manifest.c2pa) /AFRelationship %s /EF << /F 6 0 R >> >>", relationshipValue)
	specRef := "7 0 R"
	if direct {
		specRef = spec
	}
	kids := "[3 0 R]"
	if brokenPages {
		kids = "[(invalid page reference)]"
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /AF [" + specRef + "] /Names << /EmbeddedFiles 8 0 R >> >>",
		"<< /Type /Pages /Count 1 /Kids " + kids + " >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /EmbeddedFile /Subtype /application#2Fc2pa /Length 18 >>\nstream\nsynthetic manifest\nendstream",
		spec,
		"<< /Names [(manifest.c2pa) " + specRef + "] >>",
	}
	if indirectRelationship {
		objects = append(objects, "/"+relationship)
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func TestWatermarkerSupportsC2PAManifest(t *testing.T) {
	w, err := NewWatermarker()
	require.NoError(t, err)
	for _, tc := range []struct{ direct, indirectRelationship bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("direct=%v/indirectRelationship=%v", tc.direct, tc.indirectRelationship), func(t *testing.T) {
			data := c2paFixture(t, "C2PA_Manifest", tc.direct, false, tc.indirectRelationship)
			in, out := filepath.Join(t.TempDir(), "in.pdf"), filepath.Join(t.TempDir(), "out.pdf")
			require.NoError(t, os.WriteFile(in, data, 0600))
			require.NoError(t, w.Apply(context.Background(), in, out, domain.WatermarkConfig{Enabled: true, Position: "center", FontSize: 12, Opacity: .3, Color: "#888888"}, "PRIVATE test@example.com"))
			result, err := os.ReadFile(out)
			require.NoError(t, err)
			require.NotEqual(t, sha256.Sum256(data), sha256.Sum256(result))
			require.Contains(t, string(result), "/C2PA_Manifest", "retain associated-file semantics in the derived copy")
			parsed, err := api.ReadContext(bytes.NewReader(result), model.NewDefaultConfiguration())
			require.NoError(t, err)
			catalog, err := parsed.Catalog()
			require.NoError(t, err)
			af, err := parsed.DereferenceArray(catalog["AF"])
			require.NoError(t, err)
			require.Len(t, af, 1)
			spec, err := parsed.DereferenceDict(af[0])
			require.NoError(t, err)
			relationship, err := parsed.Dereference(spec["AFRelationship"])
			require.NoError(t, err)
			require.Equal(t, types.Name("C2PA_Manifest"), relationship)
			text, err := exec.Command("pdftotext", "-raw", out, "-").Output()
			require.NoError(t, err)
			require.Contains(t, strings.Join(strings.Fields(string(text)), " "), "Synthetic C2PA document")
			require.Contains(t, string(text), "PRIVATE test@example.com")
			info, err := NewPoppler().Info(context.Background(), out)
			require.NoError(t, err)
			require.Len(t, info.Pages, 1)
			require.NoError(t, NewPoppler().Page(context.Background(), out, filepath.Join(t.TempDir(), "preview.png"), 1))
			original, err := os.ReadFile(in)
			require.NoError(t, err)
			require.Equal(t, data, original)
		})
	}
}

func TestC2PARelationshipRestoredAfterValidationFailure(t *testing.T) {
	parsed, err := api.ReadContext(bytes.NewReader(c2paFixture(t, "C2PA_Manifest", false, true, false)), model.NewDefaultConfiguration())
	require.NoError(t, err)
	require.Error(t, validateWatermarkContext(parsed))
	catalog, err := parsed.Catalog()
	require.NoError(t, err)
	af, err := parsed.DereferenceArray(catalog["AF"])
	require.NoError(t, err)
	require.Len(t, af, 1)
	spec, err := parsed.DereferenceDict(af[0])
	require.NoError(t, err)
	require.Equal(t, types.Name("C2PA_Manifest"), spec["AFRelationship"])
}

func TestC2PACompatibilityKeepsPDFValidation(t *testing.T) {
	w, err := NewWatermarker()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, relationship string
		brokenPages        bool
	}{
		{"unknown relationship", "Unknown_Relationship", false},
		{"invalid page tree", "C2PA_Manifest", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, out := filepath.Join(t.TempDir(), "in.pdf"), filepath.Join(t.TempDir(), "out.pdf")
			require.NoError(t, os.WriteFile(in, c2paFixture(t, tc.relationship, false, tc.brokenPages, false), 0600))
			require.Error(t, w.Apply(context.Background(), in, out, domain.WatermarkConfig{Position: "center", FontSize: 12, Opacity: .3, Color: "#888888"}, "PRIVATE"))
			_, err := os.Stat(out)
			require.True(t, os.IsNotExist(err))
		})
	}
}
