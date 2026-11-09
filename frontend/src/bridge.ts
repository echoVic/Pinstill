import { CaptureService } from "./mygo";

// The browser preview keeps working without the native bridge. In a MyGo
// window this smoke call proves that the generated client reaches Go before
// platform-specific screen capture is connected.
try {
  await CaptureService.normalizeSelection({ X: 0, Y: 0 }, { X: 1, Y: 1 });
  const status = document.querySelector(".status");
  const label = status?.lastChild;
  if (label) label.textContent = " Go 桥接已就绪";
} catch {
  // Static browser previews do not expose the MyGo runtime.
}
