import {
  ArrowUpRight,
  Circle,
  Copy,
  Download,
  Grid2X2,
  Highlighter,
  Languages,
  Minus,
  Move,
  PenLine,
  Pin,
  QrCode,
  ScanText,
  ScrollText,
  Square,
  Stamp,
  Tag,
  Type,
  Undo2,
  X,
  createIcons,
} from "lucide";

const iconSet = {
  ArrowUpRight,
  Circle,
  Copy,
  Download,
  Grid2X2,
  Grid2x2: Grid2X2,
  Highlighter,
  Languages,
  Minus,
  Move,
  PenLine,
  Pin,
  QrCode,
  ScanText,
  ScrollText,
  Square,
  Stamp,
  Tag,
  Type,
  Undo2,
  X,
};

export function refreshTupenIcons() {
  createIcons({ icons: iconSet });
}

const root = globalThis as typeof globalThis & {
  refreshTupenIcons?: () => void;
};
root.refreshTupenIcons = refreshTupenIcons;

refreshTupenIcons();
