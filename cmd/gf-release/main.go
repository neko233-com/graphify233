// Command gf-release is the signing helper used by release.ps1 / release.sh.
// It never ships: releases contain only the graphify233 binary.
//
// Subcommands:
//
//	keygen -key release/keys/ed25519.key   generate a signing keypair
//	pubkey -key <key>                      print the base64 public key
//	checksums -dir dist                    write dist/checksums.txt
//	sign -key <key> -in checksums.txt -out checksums.txt.sig
//	verify -in checksums.txt -sig checksums.txt.sig -pub <base64>
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: gf-release keygen|pubkey|checksums|sign|verify")
	}
	switch os.Args[1] {
	case "keygen":
		keygen(os.Args[2:])
	case "pubkey":
		pubkey(os.Args[2:])
	case "checksums":
		checksums(os.Args[2:])
	case "sign":
		sign(os.Args[2:])
	case "verify":
		verify(os.Args[2:])
	default:
		fail("unknown subcommand: " + os.Args[1])
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "gf-release:", msg)
	os.Exit(1)
}

func keygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	keyPath := fs.String("key", "release/keys/ed25519.key", "private key output")
	force := fs.Bool("force", false, "overwrite an existing key")
	fs.Parse(args)
	if _, err := os.Stat(*keyPath); err == nil && !*force {
		fail("key already exists (use -force to rotate): " + *keyPath)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail(err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(*keyPath), 0o700); err != nil {
		fail(err.Error())
	}
	enc := base64.StdEncoding.EncodeToString(priv)
	if err := os.WriteFile(*keyPath, []byte(enc+"\n"), 0o600); err != nil {
		fail(err.Error())
	}
	pubPath := strings.TrimSuffix(*keyPath, filepath.Ext(*keyPath)) + ".pub"
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := os.WriteFile(pubPath, []byte(pubB64+"\n"), 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Println(pubB64)
	fmt.Fprintln(os.Stderr, "wrote private key:", *keyPath)
	fmt.Fprintln(os.Stderr, "wrote public key: ", pubPath)
}

func loadKey(path string) ed25519.PrivateKey {
	data, err := os.ReadFile(path)
	if err != nil {
		fail(err.Error())
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		fail("private key is not base64: " + err.Error())
	}
	if len(raw) != ed25519.PrivateKeySize {
		fail("private key has wrong size")
	}
	return ed25519.PrivateKey(raw)
}

func pubkey(args []string) {
	fs := flag.NewFlagSet("pubkey", flag.ExitOnError)
	keyPath := fs.String("key", "release/keys/ed25519.key", "private key")
	fs.Parse(args)
	priv := loadKey(*keyPath)
	pub, _ := priv.Public().(ed25519.PublicKey)
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
}

func checksums(args []string) {
	fs := flag.NewFlagSet("checksums", flag.ExitOnError)
	dir := fs.String("dir", "dist", "directory with release assets")
	out := fs.String("out", "", "output path (default <dir>/checksums.txt)")
	fs.Parse(args)
	entries, err := os.ReadDir(*dir)
	if err != nil {
		fail(err.Error())
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "checksums.txt" || e.Name() == "checksums.txt.sig" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(*dir, name))
		if err != nil {
			fail(err.Error())
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	path := *out
	if path == "" {
		path = filepath.Join(*dir, "checksums.txt")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Fprintln(os.Stderr, "wrote", path)
}

func sign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("key", "release/keys/ed25519.key", "private key")
	in := fs.String("in", "dist/checksums.txt", "file to sign")
	out := fs.String("out", "dist/checksums.txt.sig", "signature output")
	fs.Parse(args)
	priv := loadKey(*keyPath)
	data, err := os.ReadFile(*in)
	if err != nil {
		fail(err.Error())
	}
	sig := ed25519.Sign(priv, data)
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Fprintln(os.Stderr, "signed", *in, "->", *out)
}

func verify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	in := fs.String("in", "dist/checksums.txt", "signed file")
	sigPath := fs.String("sig", "dist/checksums.txt.sig", "signature")
	pubB64 := fs.String("pub", "", "base64 public key")
	fs.Parse(args)
	if *pubB64 == "" {
		fail("-pub is required")
	}
	pub, err := base64.StdEncoding.DecodeString(*pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		fail("invalid public key")
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		fail(err.Error())
	}
	sigRaw, err := os.ReadFile(*sigPath)
	if err != nil {
		fail(err.Error())
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigRaw)))
	if err != nil {
		fail("signature is not base64")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		fail("signature verification failed")
	}
	fmt.Println("signature ok")
}
