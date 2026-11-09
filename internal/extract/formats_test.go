package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neko233-com/graphify233/internal/model"
)

func extractText(t *testing.T, name, content string) model.Extraction {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return File(path, model.FileTypeCode)
}

func labelsOf(ex model.Extraction) []string {
	out := make([]string, 0, len(ex.Nodes))
	for _, n := range ex.Nodes {
		out = append(out, n.Label)
	}
	return out
}

func hasLabel(ex model.Extraction, want string) bool {
	for _, n := range ex.Nodes {
		if n.Label == want {
			return true
		}
	}
	return false
}

func hasRelation(ex model.Extraction, rel string) bool {
	for _, e := range ex.Edges {
		if e.Relation == rel {
			return true
		}
	}
	return false
}

func TestExtractSQL(t *testing.T) {
	ex := extractText(t, "schema.sql", `
-- comment
CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT);
CREATE TABLE orders (id SERIAL PRIMARY KEY, user_id INTEGER REFERENCES users(id));
CREATE VIEW order_summary AS SELECT u.email FROM orders o JOIN users u ON u.id = o.user_id;
`)
	for _, want := range []string{"users", "orders", "order_summary"} {
		if !hasLabel(ex, want) {
			t.Errorf("missing SQL relation %q; labels = %v", want, labelsOf(ex))
		}
	}
	if !hasRelation(ex, "references") {
		t.Errorf("missing references edge; edges = %+v", ex.Edges)
	}
}

func TestExtractTerraform(t *testing.T) {
	ex := extractText(t, "main.tf", `
variable "region" {
  default = "us-east-1"
}

resource "aws_s3_bucket" "assets" {
  bucket = "assets-${var.region}"
}

resource "aws_cloudfront_distribution" "cdn" {
  origin {
    domain_name = aws_s3_bucket.assets.bucket_regional_domain_name
  }
}

module "network" {
  source = "./modules/network"
  region = var.region
}
`)
	for _, want := range []string{"aws_s3_bucket.assets", "aws_cloudfront_distribution.cdn", "module.network", "var.region"} {
		if !hasLabel(ex, want) {
			t.Errorf("missing terraform node %q; labels = %v", want, labelsOf(ex))
		}
	}
	if !hasRelation(ex, "references") {
		t.Errorf("missing references edge; edges = %+v", ex.Edges)
	}
}

func TestExtractDockerfile(t *testing.T) {
	ex := extractText(t, "Dockerfile", `
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN go build -o /out/app ./cmd/app

FROM alpine:3.20 AS runtime
COPY --from=build /out/app /usr/local/bin/app
ENTRYPOINT ["app"]
`)
	if !hasLabel(ex, "stage build") || !hasLabel(ex, "stage runtime") {
		t.Errorf("missing stage nodes; labels = %v", labelsOf(ex))
	}
	if !hasRelation(ex, "copies_from") {
		t.Errorf("missing copies_from edge; edges = %+v", ex.Edges)
	}
}

func TestExtractOpenAPIYAML(t *testing.T) {
	ex := extractText(t, "openapi.yaml", `openapi: 3.0.3
info:
  title: Orders API
  version: 1.0.0
paths:
  /orders:
    get:
      summary: List orders
    post:
      summary: Create order
  /orders/{id}:
    get:
      summary: Read order
`)
	for _, want := range []string{"Orders API", "GET /orders", "POST /orders", "GET /orders/{id}"} {
		if !hasLabel(ex, want) {
			t.Errorf("missing OpenAPI node %q; labels = %v", want, labelsOf(ex))
		}
	}
	if !strings.HasSuffix(ex.Nodes[0].SourceFile, "openapi.yaml") {
		t.Errorf("unexpected source file %q", ex.Nodes[0].SourceFile)
	}
}

func TestExtractOpenAPIJSONIgnoredWhenNotSpec(t *testing.T) {
	ex := extractText(t, "package.json", `{"name":"x","version":"1.0.0","paths":{}}`)
	if len(ex.Nodes) != 1 {
		t.Fatalf("non-OpenAPI JSON should only yield the file node, got %v", labelsOf(ex))
	}
}
