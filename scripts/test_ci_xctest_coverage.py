"""Regression tests for XCTest coverage summaries with skipped tests."""

import unittest
from ci_xctest_coverage import passing_counts, verify_xctest


class XCTestCoverageTests(unittest.TestCase):
    def test_plain_summary(self):
        log = "** TEST SUCCEEDED **\nExecuted 78 tests, with 0 failures"
        self.assertEqual(passing_counts(log), [78])
        self.assertEqual(verify_xctest(log, "ARM"), 78)

    def test_skipped_tests_are_not_counted_as_passing(self):
        log = "** TEST SUCCEEDED **\nExecuted 78 tests, with 2 tests skipped and 0 failures (0 unexpected)"
        self.assertEqual(passing_counts(log), [76])
        self.assertEqual(verify_xctest(log, "Intel"), 76)

    def test_partial_suite_cannot_mask_missing_total(self):
        log = "** TEST SUCCEEDED **\nExecuted 19 tests, with 0 failures"
        with self.assertRaises(ValueError):
            verify_xctest(log, "ARM")

    def test_skipped_cannot_falsely_meet_coverage(self):
        log = "** TEST SUCCEEDED **\nExecuted 78 tests, with 12 tests skipped and 0 failures"
        with self.assertRaises(ValueError):
            verify_xctest(log, "Intel")

    def test_xcode_failure_must_fail_even_if_suite_looked_green(self):
        log = "** TEST FAILED **\nExecuted 78 tests, with 0 failures"
        with self.assertRaises(ValueError):
            verify_xctest(log, "Intel")

    def test_nonzero_failures_never_count(self):
        log = "** TEST SUCCEEDED **\nExecuted 78 tests, with 2 failures"
        self.assertEqual(passing_counts(log), [])
        with self.assertRaises(ValueError):
            verify_xctest(log, "ARM")

    def test_absent_total_fails(self):
        with self.assertRaises(ValueError):
            verify_xctest("** TEST SUCCEEDED **", "ARM")

    def test_single_test_grammar(self):
        self.assertEqual(passing_counts("Executed 1 test, with 0 failures"), [1])


if __name__ == "__main__":
    unittest.main()
