package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestAlipayService_loadConfigFromCertDir(t *testing.T) {
	writeFile := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	newSvc := func(certDir, appID string) *AlipayService {
		return &AlipayService{cfg: &config.Config{Alipay: config.AlipayPaymentConfig{
			AppID:    appID,
			SellerID: "2088seller",
			IsProd:   true,
			CertDir:  certDir,
		}}}
	}

	t.Run("all four files present + app_id set -> cert mode", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, certFileAppPrivateKey, "PRIV")
		writeFile(t, dir, certFileAppPublicCert, "APPCERT")
		writeFile(t, dir, certFileAlipayPubCert, "ALIPAYCERT")
		writeFile(t, dir, certFileAlipayRootCert, "ROOTCERT")

		cfg, ok := newSvc(dir, "2021app").loadConfigFromCertDir()
		if !ok {
			t.Fatal("expected ok=true")
		}
		if cfg.Mode != AlipayModeCert {
			t.Errorf("mode = %q, want %q", cfg.Mode, AlipayModeCert)
		}
		if cfg.AppID != "2021app" || cfg.SellerID != "2088seller" || !cfg.IsProd {
			t.Errorf("meta not propagated from config.yaml: %+v", cfg)
		}
		if cfg.PrivateKey != "PRIV" || cfg.AppPublicCert != "APPCERT" ||
			cfg.AlipayPublicCert != "ALIPAYCERT" || cfg.AlipayRootCert != "ROOTCERT" {
			t.Errorf("cert contents not loaded: %+v", cfg)
		}
	})

	t.Run("missing one file -> fallthrough", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, certFileAppPrivateKey, "PRIV")
		writeFile(t, dir, certFileAppPublicCert, "APPCERT")
		writeFile(t, dir, certFileAlipayPubCert, "ALIPAYCERT")
		// alipayRootCert.crt missing

		if _, ok := newSvc(dir, "2021app").loadConfigFromCertDir(); ok {
			t.Fatal("expected ok=false when a cert file is missing")
		}
	})

	t.Run("empty file -> fallthrough", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, certFileAppPrivateKey, "PRIV")
		writeFile(t, dir, certFileAppPublicCert, "APPCERT")
		writeFile(t, dir, certFileAlipayPubCert, "   ")
		writeFile(t, dir, certFileAlipayRootCert, "ROOTCERT")

		if _, ok := newSvc(dir, "2021app").loadConfigFromCertDir(); ok {
			t.Fatal("expected ok=false when a cert file is blank")
		}
	})

	t.Run("no app_id -> fallthrough even if files present", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, certFileAppPrivateKey, "PRIV")
		writeFile(t, dir, certFileAppPublicCert, "APPCERT")
		writeFile(t, dir, certFileAlipayPubCert, "ALIPAYCERT")
		writeFile(t, dir, certFileAlipayRootCert, "ROOTCERT")

		if _, ok := newSvc(dir, "").loadConfigFromCertDir(); ok {
			t.Fatal("expected ok=false when app_id is empty")
		}
	})

	t.Run("nonexistent dir -> fallthrough", func(t *testing.T) {
		if _, ok := newSvc(filepath.Join(t.TempDir(), "does-not-exist"), "2021app").loadConfigFromCertDir(); ok {
			t.Fatal("expected ok=false for nonexistent cert dir")
		}
	})
}

func TestAlipayService_certDir_default(t *testing.T) {
	s := &AlipayService{cfg: &config.Config{}}
	if got := s.certDir(); got != "./cert" {
		t.Errorf("certDir() = %q, want ./cert", got)
	}
	s.cfg.Alipay.CertDir = "  /custom/path  "
	if got := s.certDir(); got != "/custom/path" {
		t.Errorf("certDir() = %q, want /custom/path", got)
	}
}

// GetConfig 优先级：cert 目录 > config.yaml 公钥模式（无 Setting 依赖路径覆盖前两级）
func TestAlipayService_GetConfig_priority(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// config.yaml 同时配了公钥模式 + cert 目录齐全 -> 应走 cert 目录
	mustWrite(certFileAppPrivateKey, "PRIV")
	mustWrite(certFileAppPublicCert, "APPCERT")
	mustWrite(certFileAlipayPubCert, "ALIPAYCERT")
	mustWrite(certFileAlipayRootCert, "ROOTCERT")

	s := &AlipayService{cfg: &config.Config{Alipay: config.AlipayPaymentConfig{
		AppID:      "2021app",
		PrivateKey: "yaml-priv",
		PublicKey:  "yaml-pub",
		CertDir:    dir,
	}}}
	cfg, err := s.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig error: %v", err)
	}
	if cfg.Mode != AlipayModeCert {
		t.Errorf("expected cert dir to win, got mode=%q", cfg.Mode)
	}

	// cert 目录里删一个文件 -> 回落到 config.yaml 公钥模式
	if err := os.Remove(filepath.Join(dir, certFileAlipayRootCert)); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig error after removing cert: %v", err)
	}
	if cfg.Mode != AlipayModePublicKey || cfg.PrivateKey != "yaml-priv" || cfg.PublicKey != "yaml-pub" {
		t.Errorf("expected fallback to config.yaml public_key mode, got %+v", cfg)
	}
}
