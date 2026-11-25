import {
  loadGlossary,
  MAX_GLOSSARY_ENTRIES,
  saveGlossary,
  type GlossaryEntry,
} from "./translate-text";

export function renderGlossarySettings(): HTMLElement {
  const card = document.createElement("section");
  card.className = "settings-card";
  const heading = document.createElement("h3");
  heading.textContent = "自定义术语表";
  const intro = document.createElement("p");
  intro.className = "glossary-intro";
  intro.textContent = "配置专有名词的固定译法（译法留空表示保留原文不译）。使用大模型翻译时自动生效。";

  const form = document.createElement("form");
  form.id = "glossaryForm";
  form.className = "glossary-form";
  form.autocomplete = "off";

  const sourceInput = document.createElement("input");
  sourceInput.id = "glossarySource";
  sourceInput.type = "text";
  sourceInput.maxLength = 60;
  sourceInput.required = true;
  sourceInput.placeholder = "原文词条（如 Pinstill）";
  sourceInput.setAttribute("aria-label", "原文词条");

  const targetInput = document.createElement("input");
  targetInput.id = "glossaryTarget";
  targetInput.type = "text";
  targetInput.maxLength = 60;
  targetInput.placeholder = "固定译法（留空则保留原文）";
  targetInput.setAttribute("aria-label", "固定译法");

  const submit = document.createElement("button");
  submit.type = "submit";
  submit.className = "btn small";
  submit.textContent = "添加";

  form.append(sourceInput, targetInput, submit);

  const status = document.createElement("p");
  status.id = "glossaryStatus";
  status.className = "model-settings-status";
  status.setAttribute("role", "status");

  const list = document.createElement("div");
  list.id = "glossaryList";
  list.className = "glossary-list";

  card.append(heading, intro, form, status, list);

  let entries: GlossaryEntry[] = loadGlossary();

  function setStatus(text: string, kind: "info" | "success" | "error" = "info"): void {
    status.classList.toggle("error", kind === "error");
    status.classList.toggle("success", kind === "success");
    status.textContent = text;
  }

  function renderList(): void {
    list.replaceChildren();
    if (entries.length === 0) {
      const empty = document.createElement("p");
      empty.className = "glossary-empty";
      empty.textContent = "尚未添加术语。";
      list.append(empty);
      return;
    }
    for (const entry of entries) {
      const row = document.createElement("div");
      row.className = "glossary-item";
      const term = document.createElement("span");
      term.className = "glossary-term";
      const src = document.createElement("strong");
      src.textContent = entry.source;
      const arrow = document.createElement("span");
      arrow.className = "glossary-arrow";
      arrow.textContent = "→";
      const tgt = document.createElement("span");
      tgt.className = "glossary-target";
      tgt.textContent = entry.target || "保留原文";
      term.append(src, arrow, tgt);

      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "btn small";
      remove.textContent = "删除";
      remove.setAttribute("data-glossary-remove", entry.source);
      remove.addEventListener("click", () => {
        entries = saveGlossary(
          entries.filter((item) => item.source.toLowerCase() !== entry.source.toLowerCase()),
        );
        renderList();
        setStatus(`已删除「${entry.source}」`, "success");
      });

      row.append(term, remove);
      list.append(row);
    }
  }

  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const source = sourceInput.value.trim().slice(0, 60);
    const target = targetInput.value.trim().slice(0, 60);
    if (!source) {
      setStatus("请输入原文词条", "error");
      sourceInput.focus();
      return;
    }
    const existingIndex = entries.findIndex(
      (item) => item.source.toLowerCase() === source.toLowerCase(),
    );
    if (existingIndex < 0 && entries.length >= MAX_GLOSSARY_ENTRIES) {
      setStatus(`最多可添加 ${MAX_GLOSSARY_ENTRIES} 条术语`, "error");
      return;
    }
    const next = [...entries];
    if (existingIndex >= 0) {
      next[existingIndex] = { source, target };
      setStatus(`已更新「${source}」`, "success");
    } else {
      next.push({ source, target });
      setStatus(`已添加「${source}」`, "success");
    }
    entries = saveGlossary(next);
    sourceInput.value = "";
    targetInput.value = "";
    renderList();
    sourceInput.focus();
  });

  renderList();
  return card;
}
