"""Check native harness scenario producers without launching their top-level code."""
import ast
from pathlib import Path
import unittest


class ReportProducerTests(unittest.TestCase):
    # Every locally authored scenario entering completed_report must use the
    # same object schema. Imported helper results retain runtime validation.
    def test_all_completed_report_callers_emit_structured_local_scenarios(self):
        callers = 0
        rows = 0
        for path in Path(__file__).parent.glob("*_e2e.py"):
            tree = ast.parse(path.read_text(encoding="utf-8"))
            calls = [node for node in ast.walk(tree) if isinstance(node, ast.Call)
                     and isinstance(node.func, ast.Name) and node.func.id == "completed_report"]
            if not calls:
                continue
            callers += 1
            names = {call.args[1].id for call in calls if isinstance(call.args[1], ast.Name)}
            values = [call.args[1] for call in calls if not isinstance(call.args[1], ast.Name)]
            for node in ast.walk(tree):
                if (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
                        and isinstance(node.func.value, ast.Name) and node.func.value.id in names
                        and node.func.attr == "append"):
                    values.append(node.args[0])
                if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id in names for target in node.targets):
                    if isinstance(node.value, (ast.List, ast.ListComp)):
                        values.append(node.value)
            for value in values:
                expressions = value.elts if isinstance(value, ast.List) else [value.elt] if isinstance(value, ast.ListComp) else [value]
                for row in expressions:
                    rows += 1
                    with self.subTest(harness=path.name, line=row.lineno):
                        if isinstance(row, ast.Dict):
                            fields = {key.value: val for key, val in zip(row.keys, row.values) if isinstance(key, ast.Constant)}
                        elif isinstance(row, ast.Call) and isinstance(row.func, ast.Name) and row.func.id == "dict":
                            fields = {kw.arg: kw.value for kw in row.keywords}
                        else:
                            self.fail("scenario must be an object, not a bare name/string")
                        self.assertIn("name", fields)
                        self.assertIsInstance(fields.get("status"), ast.Constant)
                        self.assertEqual(fields["status"].value, "pass")
        self.assertGreaterEqual(callers, 21)
        self.assertGreater(rows, 30)


if __name__ == "__main__":
    unittest.main()
