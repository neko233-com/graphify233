# Graph Report - graphify233  (2026-11-09)

## Corpus Check
- 63 files · ~41,669 words
- ⚠️ Corpus is ~41,669 words - fits in a single context window. You may not need a graph.
- 1 sensitive file(s) skipped

## Summary
- 500 nodes · 835 edges · 35 communities
- Extraction: 62% EXTRACTED · 38% INFERRED · 0% AMBIGUOUS
- Token cost: 55,558 input · 0 output
- Mode: deep

## God Nodes (most connected - your core abstractions)
1. `run()` - 18 edges
2. `Graph` - 15 edges
3. `Engine` - 12 edges
4. `extractPython()` - 10 edges
5. `README.md` - 10 edges
6. `Detect()` - 8 edges
7. `Builder` - 8 edges
8. `Tokenize()` - 8 edges
9. `main()` - 8 edges
10. `contentText()` - 8 edges
11. `fail()` - 7 edges
12. `splitDir()` - 7 edges
13. `splitRoot()` - 7 edges
14. `Update()` - 7 edges
15. `starGraph()` - 6 edges
16. `buildCmd()` - 6 edges
17. `explainCmd()` - 6 edges
18. `graphPath()` - 6 edges
19. `loadGraph()` - 6 edges
20. `mcpCmd()` - 6 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `NewServer()`  [INFERRED]
  internal/extract/testdata/sample.c → internal/extract/testdata/sample.go  _spans files_
- `main()` --calls--> `run()`  [INFERRED]
  cmd/gf-release/main.go → cmd/graphify233/main.go  _spans files_

## Communities
### Community 0 - "sample.c"
Cohesion: 0.05
Nodes (50): extractScala(), scalaDecls(), scalaDef(), validate(), process(), main(), HttpClient, GraphifyDemo, IProcessor, DataProcessor, Server, NewServer(), (+38 more)

### Community 1 - "model.go"
Cohesion: 0.07
Nodes (40): node(), edge(), starGraph(), TestGodNodesSkipsFileNodesAndSortsByDegree(), TestSurprisingConnectionsCrossFile(), TestLabelCommunitiesAndQuestions(), smallGraph(), TestJSONRoundTrip(), TestNodeLinkShape(), Point, Layout(), placeCluster(), (+28 more)

### Community 2 - "lex.go"
Cohesion: 0.08
Nodes (37): extractC(), cFuncs(), extractCPP(), cppScope(), cppClassBody(), extractCSharp(), csDecls(), csMembers(), csCallable(), extractGo(), goFunc(), goType(), (+25 more)

### Community 3 - "main.go"
Cohesion: 0.16
Nodes (35): main(), fail(), keygen(), loadKey(), pubkey(), checksums(), sign(), verify(), run(), usage(), splitRoot(), isFlagValue(), (+23 more)

### Community 4 - "mcp.go"
Cohesion: 0.12
Nodes (25): request, rpcError, response, toolCall, tool, Serve(), handle(), tools(), callTool(), Engine, Terms()

### Community 5 - "misc.go"
Cohesion: 0.10
Nodes (22): WriteCypher(), cypherLabel(), cypherStr(), graphmlDoc, graphmlKey, graphmlGraph, graphmlNode, graphmlEdge, graphmlData, WriteSVG(), xmlEscape(), canvasDoc, (+10 more)

### Community 6 - "analyze.go"
Cohesion: 0.18
Nodes (20): GodNode, Surprise, confRank(), SurprisingConnections(), crossFileSurprises(), crossCommunitySurprises(), topDir(), SuggestedQuestions(), describeCommunities(), LabelCommunities(), Betweenness(), itoa(), (+8 more)

### Community 7 - "pdf.go"
Cohesion: 0.19
Nodes (19): extractPaper(), truncateRunes(), firstSentence(), ExtractText(), ExtractTextBytes(), decodeStream(), looksLikeContent(), contentText(), applyGap(), readLiteral(), readHex(), decodePDFString(), (+7 more)

### Community 8 - "extract.go"
Cohesion: 0.17
Nodes (19): MakeID(), Builder, NewBuilder(), normaliseLabel(), File(), isPunct(), FuncSpan, ScanCalls()

### Community 9 - "pipeline.go"
Cohesion: 0.16
Nodes (18): Formats, Options, Result, fileJob, Run(), reportRoot(), displayRel(), Data, Write(), Render(), knowledgeGaps(), confidenceCounts(), (+6 more)

### Community 10 - "detect.go"
Cohesion: 0.23
Nodes (16): Kind, Result, IsSensitive(), isNoiseDir(), Classify(), Detect(), humanInt(), displayPath(), walkPruned(), walkAll(), wordCountFile(), isSpaceByte(), (+4 more)

### Community 11 - "python.go"
Cohesion: 0.25
Nodes (16): extractPython(), inRange(), splitLines(), pyMasker, stripPyComment(), pyIndent(), parsePyClassName(), parsePyFuncName(), isIdentByte(), matchParenString(), splitTopLevel(), parsePyImport(), (+4 more)

### Community 12 - "Graph"
Cohesion: 0.30
Nodes (15): Graph

### Community 13 - "cache.go"
Cohesion: 0.24
Nodes (12): Dir(), Get(), Put(), FileEntry, Manifest, ManifestPath(), LoadManifest(), SaveManifest(), Clear()

### Community 14 - "cluster.go"
Cohesion: 0.30
Nodes (12): level, Detect(), sortCommunities(), splitOversized(), induced(), louvainOn(), louvain(), runLevels(), aggregate(), localMove()

### Community 15 - "selfupdate.go"
Cohesion: 0.30
Nodes (12): AssetName(), release, client(), assetURL(), Latest(), Check(), findAsset(), Update(), fetch(), hashFor()

### Community 16 - "README.md"
Cohesion: 0.18
Nodes (11): README.md, graphify233, Install, Mirrors (restricted networks), Quick start, Commands, Languages, MCP, Performance, Security, Development

### Community 17 - "javascript.go"
Cohesion: 0.35
Nodes (11): extractJS(), jsClassName(), jsClass(), jsMethods(), jsMethodStart(), jsSkipGenerics(), jsFunction(), jsImport(), jsLexical(), jsSkipType()

### Community 18 - "benchmark.go"
Cohesion: 0.33
Nodes (9): Question, Report, EstimateTokens(), QuerySubgraphTokens(), Run(), Print(), truncate(), commas()

### Community 19 - "graphjson.go"
Cohesion: 0.31
Nodes (9): nodeLink, jsonNode, jsonLink, ToNodeLink(), buildNodeLink(), WriteJSON(), EncodeJSON()

### Community 20 - "html.go"
Cohesion: 0.31
Nodes (9): vizNode, vizLink, vizCommunity, vizPayload, WriteHTML(), round1(), round2(), communityColor()

### Community 21 - "install.go"
Cohesion: 0.33
Nodes (9): Paths(), Claude(), RemoveClaude(), GuidanceFiles(), WriteGuidance(), RemoveGuidance(), vscodeFile

### Community 22 - "extract_test.go"
Cohesion: 0.36
Nodes (8): goldenNode, goldenEdge, goldenFile, loadGolden(), TestGoldenParity(), compareNodes(), compareEdges()

### Community 23 - "ruby.go"
Cohesion: 0.50
Nodes (8): extractRuby(), stripRubyComment(), rubyIndent(), rubyClassName(), rubyDefName(), rubyBlockEnd(), rubyOpensBlock()

### Community 24 - "sample_calls.py"
Cohesion: 0.46
Nodes (8): compute_score(), normalize(), run_analysis(), Analyzer

### Community 25 - "cluster_test.go"
Cohesion: 0.43
Nodes (7): twoCliques(), TestDetectPartitionsEveryNode(), fourCliques(), TestDetectMultiLevelKeepsGroups(), TestDetectDeterministic()

### Community 26 - "obsidian.go"
Cohesion: 0.52
Nodes (7): WriteObsidian(), cohesionSafe(), dominantConfidence(), safeName(), safeTag()

### Community 27 - "java.go"
Cohesion: 0.60
Nodes (6): extractJava(), javaImports(), javaDecls(), javaMembers(), javaCallable()

### Community 28 - "rust.go"
Cohesion: 0.60
Nodes (6): extractRust(), rustScope(), rustFunc(), rustImpl(), rustUse()

### Community 29 - "hooks.go"
Cohesion: 0.60
Nodes (6): Dir(), Path(), Install(), Uninstall(), Status()

### Community 30 - "kotlin.go"
Cohesion: 0.70
Nodes (5): extractKotlin(), ktDecls(), ktFuncName(), lineEnd()

### Community 31 - "php.go"
Cohesion: 0.80
Nodes (5): extractPHP(), phpDecls(), phpMembers(), phpFunc()

### Community 32 - "pipeline_test.go"
Cohesion: 0.67
Nodes (4): writeCorpus(), TestRunWritesArtifactsAndCaches()

### Community 33 - "CLAUDE.md"
Cohesion: 1.00
Nodes (2): CLAUDE.md, graphify233

### Community 34 - "gen_goldens.py"
Cohesion: 1.00
Nodes (2): gen_goldens.py, main()

## Ambiguous Edges - Review These
- None - every edge has a clear source.

## Knowledge Gaps
- **2 thin communities (size < 3)** - likely incomplete extractions

## Suggested Questions
- How does `model.go` connect the different parts of the system (bridges sample.c and lex.go)?
- How does `main.go` connect the different parts of the system (bridges sample.c and model.go)?
- How does `scala.go` connect the different parts of the system (bridges model.go and lex.go)?
- Are the 17 inferred relationships involving `run()` correct?
- Are the 10 inferred relationships involving `.Len()` correct?
- Is "sample.c" really one module? Cohesion is only 0.05.
- Is "model.go" really one module? Cohesion is only 0.07.
