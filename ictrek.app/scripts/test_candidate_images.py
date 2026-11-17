import importlib.util
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest


spec = importlib.util.spec_from_file_location("candidate_images", Path(__file__).with_name("candidate_images.py"))
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


class CandidateImagesTest(unittest.TestCase):
    def test_preserves_explicit_versions_and_pinned_references(self):
        with TemporaryDirectory() as directory:
            source, target = Path(directory) / "input", Path(directory) / "output"
            image = "registry.example.com/team/app:arm_candidate@sha256:" + "a" * 64
            source.write_text("# selected candidate\nAPP_ARM_IMAGE=" + image + "\nAPP_AMD_IMAGE=registry.example.com/team/app:amd_released\n")
            candidate.write_candidate_images(source, target, ["APP_AMD_IMAGE", "APP_ARM_IMAGE"])
            self.assertEqual(target.read_text().splitlines(), ["APP_AMD_IMAGE=registry.example.com/team/app:amd_released", "APP_ARM_IMAGE=" + image])

    def test_rejects_incomplete_ambiguous_and_executable_input_without_overwriting(self):
        cases = [
            "",
            "APP_ARM_IMAGE=registry.example.com/team/app:first\nAPP_ARM_IMAGE=registry.example.com/team/app:second\n",
            "SECRET=do-not-copy\nAPP_ARM_IMAGE=registry.example.com/team/app:first\n",
            "APP_ARM_IMAGE=local/app:first\n",
            "APP_ARM_IMAGE=registry.example.com/team/app:$(touch marker)\n",
            'APP_ARM_IMAGE="registry.example.com/team/app:first"\n',
            "APP_ARM_IMAGE=registry.example.com/team/app:first # comment\n",
        ]
        for text in cases:
            with self.subTest(text=text), TemporaryDirectory() as directory:
                source, target = Path(directory) / "input", Path(directory) / "output"
                source.write_text(text)
                target.write_text("unchanged")
                with self.assertRaises(ValueError):
                    candidate.write_candidate_images(source, target, ["APP_ARM_IMAGE"])
                self.assertEqual(target.read_text(), "unchanged")


if __name__ == "__main__":
    unittest.main()
