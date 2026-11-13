"""python3 -m unittest -v test_triage   （在本目录下跑，只用标准库）"""
import collections
import json
import os
import tempfile
import threading
import unittest
import urllib.request
from http.server import ThreadingHTTPServer

import triage

HERE = os.path.dirname(os.path.abspath(__file__))
RULES = triage.load_rules(os.path.join(HERE, "rules.json"))


def alert(alertname, severity="critical", status="firing", **labels):
    labels.update(alertname=alertname, severity=severity, service="sub2api", env="prod")
    return {"status": status, "labels": labels, "startsAt": "2026-11-12T06:54:00Z", "endsAt": "2026-11-12T07:02:00Z",
            "annotations": {"summary": "摘要", "description": "排查命令"}}


class FakeMailer:
    def __init__(self):
        self.sent = []

    def send(self, subject, body):
        self.sent.append((subject, body))


class FakeGitHub:
    def __init__(self):
        self.issues, self.comments, self.closed = {}, collections.defaultdict(list), []

    def create_issue(self, title, body, labels):
        number = len(self.issues) + 1
        self.issues[number] = {"title": title, "body": body, "labels": list(labels), "state": "open"}
        return number

    def comment(self, number, body):
        self.comments[number].append(body)

    def issue(self, number):
        item = self.issues[number]
        return {"state": item["state"], "labels": item["labels"], "url": "https://example.invalid/issues/%d" % number}

    def last_comment(self, number):
        return self.comments[number][-1] if self.comments[number] else ""

    def close(self, number):
        self.issues[number]["state"] = "closed"
        self.closed.append(number)


class Harness:
    """一套不碰网络的 Triage：指标由 evidence 字典伪造，时间可拨。"""

    def __init__(self, mode="notify", evidence=None, cap=5):
        self.clock = 1_800_000_000.0
        self.evidence = evidence or {}
        self.mailer, self.github = FakeMailer(), FakeGitHub()
        cfg = triage.Config()
        cfg.mode, cfg.service, cfg.cooldown, cfg.daily_ai_cap, cfg.pg_dsn = mode, "sub2api", 6 * 3600, cap, ""
        self.triage = triage.Triage(cfg, triage.Store(":memory:"), RULES, self.mailer, self.github,
                                    query=self._query, now=lambda: self.clock)

    def _query(self, promql, at=None):
        for name, key, label in (("sub2api_upstream_status_total", "upstream_status", "upstream_status"),
                                 ("by (phase)", "error_phase", "phase"), ("by (type)", "error_type", "type")):
            if name in promql:
                return [{"metric": {label: k}, "value": [0, str(v)]} for k, v in self.evidence.get(key, {}).items()]
        for name, key in (("probe_model_http_status", "probe_status"), ("probe_model_metered", "probe_metered")):
            if name in promql and key in self.evidence:
                return [{"metric": {}, "value": [0, str(self.evidence[key])]}]
        return []

    def post(self, *alerts):
        return self.triage.handle_webhook({"alerts": list(alerts)})


class ReplayTest(unittest.TestCase):
    """2026-11 生产上真实触发过的 47 次 sub2api 告警 + 当时 Prometheus 里的证据。"""

    @classmethod
    def setUpClass(cls):
        with open(os.path.join(HERE, "fixtures", "replay-2026-11.json"), encoding="utf-8") as f:
            cls.cases = json.load(f)

    def test_every_episode_matches_recorded_verdict(self):
        self.assertEqual(len(self.cases), 47)
        for case in self.cases:
            verdict = triage.classify(case["labels"], case["evidence"], RULES)
            self.assertEqual({"rule": verdict["rule"], "route": verdict["route"]}, case["expect"],
                             "%s %s" % (case["started"], case["labels"]["alertname"]))

    def test_all_critical_go_to_a_human(self):
        critical = [c for c in self.cases if c["labels"]["severity"] == "critical"]
        self.assertEqual(len(critical), 24)
        self.assertEqual({c["expect"]["route"] for c in critical}, {"human"})

    def test_auth_rejected_is_never_sent_to_ai(self):
        rejected = [c for c in self.cases if c["labels"]["alertname"] == "Sub2apiUpstreamAuthRejected"]
        self.assertEqual(len(rejected), 5)
        for case in rejected:
            self.assertEqual(case["expect"], {"rule": "upstream-auth-alert", "route": "human"})

    def test_route_totals(self):
        totals = collections.Counter(c["expect"]["route"] for c in self.cases)
        self.assertEqual(totals, {"human": 24, "log": 21, "autofix": 2})


class ClassifyTest(unittest.TestCase):
    def verdict(self, alertname, evidence=None, severity="critical", **labels):
        labels.update(alertname=alertname, severity=severity)
        return triage.classify(labels, evidence or {}, RULES)

    def test_probe_ok_but_not_metered_is_a_code_bug(self):
        v = self.verdict("Sub2apiProbeModelFailing", {"probe_status": 200, "probe_metered": 0}, model="glm-5.2")
        self.assertEqual((v["rule"], v["route"]), ("probe-not-metered", "autofix"))

    def test_probe_402_is_billing(self):
        v = self.verdict("Sub2apiProbeModelFailing", {"probe_status": 402, "probe_metered": 0}, model="glm-5.2")
        self.assertEqual((v["rule"], v["route"]), ("probe-auth", "human"))

    def test_all_failing_with_402_majority_is_billing(self):
        v = self.verdict("Sub2apiUpstreamAllFailing", {"upstream_status": {"402": 20, "500": 2}})
        self.assertEqual((v["rule"], v["route"]), ("any-upstream-auth-majority", "human"))

    def test_all_failing_with_400_majority_goes_to_ai(self):
        v = self.verdict("Sub2apiUpstreamAllFailing", {"upstream_status": {"400": 30, "200": 500}})
        self.assertEqual((v["rule"], v["route"]), ("upstream-bad-request-majority", "autofix"))

    def test_all_failing_with_5xx_is_upstream(self):
        v = self.verdict("Sub2apiUpstreamAllFailing", {"upstream_status": {"503": 30}})
        self.assertEqual((v["rule"], v["route"]), ("upstream-failure", "human"))

    def test_infra_and_stuck_jobs(self):
        self.assertEqual(self.verdict("Sub2apiSiteDown")["route"], "human")
        self.assertEqual(self.verdict("Sub2apiUsageNotRecorded")["route"], "autofix")
        self.assertEqual(self.verdict("Sub2apiSubscriptionExpiryStuck", severity="warning")["route"], "autofix")

    def test_internal_errors_without_upstream_cause_go_to_ai(self):
        v = self.verdict("Sub2apiInternalErrorSpike", {"upstream_status": {"200": 900}, "error_phase": {"internal": 120}},
                         severity="warning")
        self.assertEqual((v["rule"], v["route"]), ("error-ratio-code", "autofix"))

    def test_unknown_alert(self):
        self.assertEqual(self.verdict("Sub2apiSomethingNew")["route"], "autofix")
        self.assertEqual(self.verdict("Sub2apiSomethingNew", severity="warning")["route"], "log")

    def test_every_rule_has_required_fields(self):
        ids = [r["id"] for r in RULES]
        self.assertEqual(len(ids), len(set(ids)))
        for rule in RULES:
            self.assertTrue(rule["category"] and rule["advice"], rule["id"])
            routes = rule["route"].values() if isinstance(rule["route"], dict) else [rule["route"]]
            self.assertTrue(set(routes) <= {"human", "autofix", "log"}, rule["id"])
        self.assertNotIn("alerts", RULES[-1])  # 最后一条必须是兜底


class FlowTest(unittest.TestCase):
    def test_billing_alert_mails_once_and_ignores_repeats(self):
        h = Harness()
        first = h.post(alert("Sub2apiUpstreamAuthRejected", platform="anthropic", model="__other__"))
        again = h.post(alert("Sub2apiUpstreamAuthRejected", platform="anthropic", model="__other__"))
        self.assertEqual(first[0]["action"], "mailed")
        self.assertEqual(again[0]["action"], "duplicate")
        self.assertEqual(len(h.mailer.sent), 1)
        subject, body = h.mailer.sent[0]
        self.assertEqual(subject, "[sub2api 需人工] Sub2apiUpstreamAuthRejected（__other__）")
        self.assertIn("上游欠费 / 密钥失效 / 封号", body)
        self.assertEqual(h.github.issues, {})

    def test_flapping_alert_is_suppressed_during_cooldown(self):
        h = Harness()
        fire = alert("Sub2apiNoAvailableAccount", platform="anthropic", model="gpt-5.5")
        gone = alert("Sub2apiNoAvailableAccount", status="resolved", platform="anthropic", model="gpt-5.5")
        self.assertEqual(h.post(fire)[0]["action"], "mailed")
        self.assertEqual(h.post(gone)[0]["action"], "resolved")
        h.clock += 1800
        self.assertEqual(h.post(fire)[0]["action"], "mail-suppressed")
        h.post(gone)
        h.clock += 6 * 3600
        self.assertEqual(h.post(fire)[0]["action"], "mailed")
        self.assertEqual(len(h.mailer.sent), 2)

    def test_warning_noise_is_only_logged(self):
        h = Harness(evidence={"upstream_status": {"200": 600, "429": 50}})
        result = h.post(alert("Sub2apiModelHighLatency", severity="warning", platform="anthropic", model="__other__"))
        self.assertEqual((result[0]["route"], result[0]["action"]), ("log", "logged"))
        self.assertEqual(h.mailer.sent, [])

    def test_other_services_are_ignored(self):
        h = Harness()
        other = alert("Sub2apiUpstreamAuthRejected")
        other["labels"]["service"] = "tassub2api"
        self.assertEqual(h.post(other), [])
        self.assertEqual(h.mailer.sent, [])

    def test_notify_mode_mails_code_candidates_instead_of_opening_issues(self):
        h = Harness(mode="notify")
        result = h.post(alert("Sub2apiUsageNotRecorded"))
        self.assertEqual(result[0]["action"], "mailed")
        self.assertTrue(h.mailer.sent[0][0].startswith("[sub2api 代码类候选]"))
        self.assertEqual(h.github.issues, {})

    def test_autofix_mode_opens_one_issue_and_reuses_it(self):
        h = Harness(mode="autofix")
        fire = alert("Sub2apiUsageNotRecorded")
        self.assertEqual(h.post(fire)[0]["action"], "issue-created")
        self.assertEqual(h.github.issues[1]["labels"], ["alert", "auto-fix"])
        self.assertIn("不可信数据", h.github.issues[1]["body"])
        h.post(alert("Sub2apiUsageNotRecorded", status="resolved"))
        self.assertIn("告警已恢复", h.github.comments[1][-1])
        self.assertEqual(h.post(fire)[0]["action"], "issue-commented")
        self.assertEqual(len(h.github.issues), 1)
        self.assertEqual(h.mailer.sent, [])

    def test_daily_ai_cap_falls_back_to_mail(self):
        h = Harness(mode="autofix", cap=1)
        self.assertEqual(h.post(alert("Sub2apiUsageNotRecorded"))[0]["action"], "issue-created")
        self.assertEqual(h.post(alert("Sub2apiProbeStale", severity="warning"))[0]["action"], "mailed")
        self.assertEqual(len(h.github.issues), 1)
        self.assertIn("已达上限", h.mailer.sent[0][1])

    def test_poll_reports_pr_and_needs_human(self):
        h = Harness(mode="autofix")
        h.post(alert("Sub2apiUsageNotRecorded"), alert("Sub2apiProbeStale", severity="warning"))
        h.triage.poll_issues()
        self.assertEqual(h.mailer.sent, [])  # AI 还没给结论

        h.github.issues[1]["labels"].append("pr-opened")
        h.github.comment(1, "根因：…… PR: https://example.invalid/pull/7")
        h.github.issues[2]["labels"].append("needs-human")
        h.github.comment(2, "不是代码问题：上游 key sk-abcdefgh12345678 已失效")
        h.triage.poll_issues()
        subjects = sorted(s for s, _ in h.mailer.sent)
        self.assertEqual(subjects, ["[sub2api AI无法修复] Sub2apiProbeStale", "[sub2api 已提PR] Sub2apiUsageNotRecorded"])
        self.assertEqual(h.github.closed, [2])
        self.assertNotIn("sk-abcdefgh12345678", "".join(b for _, b in h.mailer.sent))

        h.triage.poll_issues()
        self.assertEqual(len(h.mailer.sent), 2)  # 结论只报一次

    def test_failed_mail_is_not_recorded_so_alertmanager_retries(self):
        h = Harness()

        def boom(subject, body):
            raise RuntimeError("smtp down")
        h.mailer.send = boom
        with self.assertRaises(RuntimeError):
            h.post(alert("Sub2apiSiteDown"))
        h.mailer = h.triage.mailer = FakeMailer()
        self.assertEqual(h.post(alert("Sub2apiSiteDown"))[0]["action"], "mailed")


class RedactTest(unittest.TestCase):
    def test_secrets_and_pii_are_removed(self):
        text = triage.redact("key sk-live_abcDEF123456 Bearer eyJhbGciOi.payload.sig user a.b@example.com tel 13800138000 "
                             "api_key=zzz999 blob " + "A" * 48)
        for leaked in ("sk-live_abcDEF123456", "eyJhbGciOi", "a.b@example.com", "13800138000", "zzz999", "A" * 48):
            self.assertNotIn(leaked, text)

    def test_plain_error_text_survives_and_is_truncated(self):
        self.assertEqual(triage.redact("upstream returned 402 insufficient balance"), "upstream returned 402 insufficient balance")
        self.assertEqual(triage.redact("x" * 30, limit=10), "x" * 10 + "…")


class HTTPTest(unittest.TestCase):
    def test_webhook_endpoint(self):
        h = Harness()
        server = ThreadingHTTPServer(("127.0.0.1", 0), triage.make_handler(h.triage))
        threading.Thread(target=server.serve_forever, daemon=True).start()
        base = "http://127.0.0.1:%d" % server.server_address[1]
        try:
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            self.assertEqual(json.load(opener.open(base + "/healthz"))["status"], "ok")
            body = json.dumps({"alerts": [alert("Sub2apiPostgresDown")]}).encode()
            req = urllib.request.Request(base + "/alert", data=body, headers={"Content-Type": "application/json"})
            result = json.load(opener.open(req))["results"]
            self.assertEqual(result[0]["action"], "mailed")
        finally:
            server.shutdown()
            server.server_close()

    def test_store_persists_on_disk(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "triage.db")
            store = triage.Store(path)
            store.add(fingerprint="a||", alertname="a", severity="critical", labels="{}", rule="infra", category="c",
                      route="human", status="firing", started_at=1.0)
            self.assertIsNotNone(triage.Store(path).open_incident("a||"))


if __name__ == "__main__":
    unittest.main()
