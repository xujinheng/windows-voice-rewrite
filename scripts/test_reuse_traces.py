"""Contract tests: fake replay streams test the verifier, NOT production parity."""
import copy
import unittest

import reuse_traces as t


class TraceContractTests(unittest.TestCase):
    def test_reviewed_scenarios_are_frozen(self):
        cases = t.load_fixtures()
        self.assertEqual(len(cases), 8)
        self.assertTrue(all(c["required_effects"] for c in cases))

    def test_parity_rejects_empty_effects_and_invented_credit(self):
        case = t.load_fixtures()[0]
        with self.assertRaisesRegex(t.TraceError, "effect not executed"):
            t.check_case(case, {"v": 1, "effects": []},
                         {"v": 1, "effects": []})

    def test_parity_rejects_mismatch(self):
        case = t.load_fixtures()[0]
        effects = [{"type": k} for k in case["required_effects"]]
        with self.assertRaisesRegex(t.TraceError, "differ"):
            t.check_case(case, {"v": 1, "effects": effects},
                         {"v": 1, "effects": effects[:-1]})

    def test_parity_rejects_forbidden_effect(self):
        case = next(c for c in t.load_fixtures() if c["name"] == "cancel_stale_asr")
        effects = [{"type": k} for k in case["required_effects"]] + [
            {"type": "attempt_native_write"}]
        with self.assertRaisesRegex(t.TraceError, "forbidden effect"):
            t.check_case(case, {"v": 1, "effects": effects},
                         {"v": 1, "effects": effects})

    def test_native_execution_receipt_is_the_only_ignored_effect(self):
        case = t.load_fixtures()[0]
        effects = [{"type": k} for k in case["required_effects"]]
        other = copy.deepcopy(effects)
        other.append({"type": "native_execution_receipt", "platform": "macos"})
        t.check_case(case, {"v": 1, "effects": effects},
                     {"v": 1, "effects": other})

    def test_v1_wire_contract_has_required_receipt_source(self):
        cases = t.load_fixtures()
        dictation = next(x for x in cases if x["name"] == "happy_dictation")
        ready = next(x for x in dictation["events"] if x["type"] == "record_ready")
        self.assertEqual(ready["receipt_from"], "start_native_capture")
        self.assertEqual(ready["v"], 1)
        ready.pop("receipt_from")
        with self.assertRaisesRegex(t.TraceError, "receipt requires"):
            with __import__("tempfile").TemporaryDirectory() as d:
                from pathlib import Path
                file = Path(d) / "traces.jsonl"
                file.write_text("\n".join(
                    __import__("json").dumps(x) for x in cases) + "\n")
                t.load_fixtures(file)

    def test_uncertain_write_receipt_requires_exact_preview_count(self):
        case = next(x for x in t.load_fixtures() if x["name"] == "uncertain_paste")
        effects = [{"type": kind} for kind in case["required_effects"]]
        with self.assertRaisesRegex(t.TraceError, "show_preview expected 2"):
            t.check_case(case, {"v": 1, "effects": effects},
                         {"v": 1, "effects": effects})

    def test_full_payload_not_just_kind_matches(self):
        case = t.load_fixtures()[0]
        effects = [{"type": k, "session_id": "s1"} for k in case["required_effects"]]
        other = copy.deepcopy(effects)
        other[0]["session_id"] = "wrong"
        with self.assertRaisesRegex(t.TraceError, "differ"):
            t.check_case(case, {"v": 1, "effects": effects},
                         {"v": 1, "effects": other})


if __name__ == "__main__":
    unittest.main()
