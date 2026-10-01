package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goMain = `package main

import (
	"fmt"
	"os"
)

import "strings"

type Server struct{}
type internalOnly struct{}

var Version = "dev"
const Limit = 3

func Main() {}
func (s *Server) Start() {}
func helper() {}
`

const tsIndex = `import express from "express";
import { local } from "./local";
const fs = require("fs");
export default async function handler() {}
export class Router {}
export const port = 3000;
module.exports = handler;
`

const pyApp = `import os.path
from flask import Flask
from .views import index
def create_app():
    pass
def _private():
    pass
class Config:
    pass
class _Hidden:
    pass
`

const rustLib = `use std::io;
use serde::Serialize;
pub fn run() {}
pub async fn serve() {}
pub struct Server;
pub enum Mode { A }
pub trait Handler {}
pub mod nested;
`

// projectFiles writes a small project with an important file of each language.
func projectFiles(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"main.go": goMain, "index.ts": tsIndex, "app.py": pyApp, "src/lib.rs": rustLib,
		"package.json": `{"name":"x"}`, ".github/workflows/ci.yml": "on: push\n", "notes.txt": "not important\n",
		"README.md": strings.Repeat("line\n", 600),
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestAnalyzeImportantFiles(t *testing.T) {
	root := projectFiles(t)
	tree, err := ExploreProject(root, ExplorerOptions{IncludeHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	analyses := AnalyzeImportantFiles(root, tree)
	byPath := map[string]FileAnalysis{}
	for i, a := range analyses {
		byPath[a.Path] = a
		if i > 0 && a.Importance > analyses[i-1].Importance {
			t.Fatalf("not sorted by importance: %+v", analyses)
		}
	}
	if len(analyses) != 7 {
		t.Fatalf("%d analyses: %+v (notes.txt is not important)", len(analyses), analyses)
	}
	checks := []struct {
		path, language, imports, exports, summary string
		lines                                     int
	}{
		{"main.go", "go", "fmt,os,strings", "Server,Version,Limit,Main,Start", "entry_point | 18 lines | 5 exports", 18},
		{"index.ts", "typescript", "express,fs", "handler,Router,port,module.exports",
			"entry_point | 7 lines | 4 exports", 7},
		{"app.py", "python", "os,flask", "create_app,Config", "entry_point | 11 lines | exports: create_app, Config", 11},
		{filepath.Join("src", "lib.rs"), "rust", "std,serde", "run,serve,Server,Mode,Handler",
			"entry_point | 8 lines | 5 exports", 8},
		{"package.json", "json", "", "", "config | 1 lines", 1},
		{filepath.Join(".github", "workflows", "ci.yml"), "yaml", "", "", "ci | 1 lines", 1},
	}
	for _, c := range checks {
		a, ok := byPath[c.path]
		if !ok {
			t.Errorf("%s was not analysed", c.path)
			continue
		}
		if a.Language != c.language || strings.Join(a.Imports, ",") != c.imports || strings.Join(a.Exports, ",") != c.exports ||
			a.Summary != c.summary || a.LineCount != c.lines {
			t.Errorf("%s: %+v", c.path, a)
		}
	}
	// Past the 500 lines that are scanned for imports the rest are only counted.
	if readme := byPath["README.md"]; readme.Language != "markdown" || readme.LineCount <= 500 ||
		!strings.HasPrefix(readme.Summary, "documentation | ") {
		t.Errorf("README.md: %+v", readme)
	}
}

func TestAnalyzeImportantFilesSkipsAFileItCannotOpen(t *testing.T) {
	tree := &DirectoryTree{IsDir: true, Children: []*DirectoryTree{{Name: "main.go", Path: "main.go"}}}
	if got := AnalyzeImportantFiles(t.TempDir(), tree); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	if got := AnalyzeImportantFiles(t.TempDir(), nil); len(got) != 0 {
		t.Fatalf("a nil tree: %+v", got)
	}
}

func TestDeduplicateStrings(t *testing.T) {
	if got := strings.Join(deduplicateStrings([]string{"a", "b", "a", "c", "d"}, 3), ","); got != "a,b,c" {
		t.Fatalf("deduplicateStrings() = %s", got)
	}
}

func TestGetDirImportance(t *testing.T) {
	if GetDirImportance("CMD") != 9 || GetDirImportance("src") != 8 || GetDirImportance("misc") != 0 {
		t.Fatal("importance by lower-cased name, 0 when unknown")
	}
}
