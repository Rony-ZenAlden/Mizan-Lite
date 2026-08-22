import { useMemo, useState } from "react";
import { useTranslation } from "@/app/providers/PreferencesProvider";
import { Badge, Button } from "@/shared/ui";
import {
  CHAPTERS,
  FIRST_SECTION,
  type Block,
  type Language,
  type Section,
} from "@/modules/help/content";

/**
 * The in-app guide (Step 10.9).
 *
 * # Why the guide has its own language toggle
 *
 * Everything else in Mizan follows the locale the user picked once, and should. A guide is the
 * exception, for a reason that comes from the room rather than from the code: the person reading
 * it is often not the person the application is set up for. An Arabic-speaking shopkeeper hands
 * the laptop to a bilingual relative; an English-speaking accountant is shown a screen configured
 * in Arabic.
 *
 * Making them change the application's language to read a paragraph — and change it back — is a
 * worse answer than one button. It DEFAULTS to the application's language, so nobody who does not
 * need it ever notices it.
 */
export function HelpScreen() {
  const { t, locale } = useTranslation();

  // # Why this FOLLOWS the application's locale until the reader chooses
  //
  // Seeding it once from `locale` looked right and was wrong: the state initialiser runs at mount,
  // and the provider has not loaded preferences yet, so an application set to Arabic opened the
  // guide in English every time. A test caught it; a user would have called it broken.
  //
  // `chosen` is what separates "the reader picked a language" from "the reader has not touched
  // it". Until they do, the guide follows the application. Once they do, it stops — because a
  // deliberate choice being overwritten by a background load is the more annoying bug of the two.
  const [chosen, setChosen] = useState<Language | null>(null);
  const language: Language = chosen ?? (locale === "ar" ? "ar" : "en");
  const setLanguage = setChosen;
  // The first section of the first chapter. `FIRST_SECTION` is derived once in the content module
  // rather than indexed here, so a reordered guide cannot leave this pointing at nothing.
  const [openSection, setOpenSection] = useState<string>(FIRST_SECTION);

  const rtl = language === "ar";
  const contents = useMemo(
    () => CHAPTERS.flatMap((chapter) => chapter.sections.map((s) => s.id)),
    [],
  );
  const position = contents.indexOf(openSection);

  return (
    /*
     * `dir` is set on this subtree, not on the document.
     *
     * Reading the guide in Arabic while the application runs in English must not flip the menu,
     * the toolbar, or the screen behind it — a guide that rearranges the application around
     * itself is worse than one in the wrong language.
     */
    <section dir={rtl ? "rtl" : "ltr"} className="flex flex-col gap-6">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h2 className="text-base font-medium text-text">
            {rtl ? "دليل ميزان" : "The Mizan guide"}
          </h2>
          <p className="text-sm text-text-muted">
            {rtl
              ? "كيف يعمل البرنامج، وكيف تنساب البيانات، وماذا تفعل يومياً."
              : "How it works, where your data goes, and what to do day to day."}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-xs text-text-muted">{t("help.language")}</span>
          <Button
            variant={language === "en" ? "primary" : "ghost"}
            size="sm"
            onClick={() => setLanguage("en")}
          >
            English
          </Button>
          <Button
            variant={language === "ar" ? "primary" : "ghost"}
            size="sm"
            onClick={() => setLanguage("ar")}
          >
            العربية
          </Button>
        </div>
      </header>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[16rem_1fr]">
        {/* ── contents ──────────────────────────────────────────────────────── */}
        <nav
          aria-label={rtl ? "المحتويات" : "Contents"}
          className="flex flex-col gap-4 lg:sticky lg:top-4 lg:self-start"
        >
          {CHAPTERS.map((chapter) => (
            <div key={chapter.id} className="flex flex-col gap-1">
              <h3 className="text-xs font-medium uppercase tracking-wide text-text-muted">
                {rtl ? chapter.titleAr : chapter.titleEn}
              </h3>
              {chapter.sections.map((section) => (
                <button
                  key={section.id}
                  type="button"
                  onClick={() => setOpenSection(section.id)}
                  aria-current={openSection === section.id ? "true" : undefined}
                  className={`rounded-md px-2 py-1 text-start text-sm ${
                    openSection === section.id
                      ? "bg-surface-muted font-medium text-text"
                      : "text-text-muted hover:bg-surface-muted"
                  }`}
                >
                  {rtl ? section.titleAr : section.titleEn}
                </button>
              ))}
            </div>
          ))}
        </nav>

        {/* ── the open section ──────────────────────────────────────────────── */}
        <div className="flex flex-col gap-4">
          {CHAPTERS.flatMap((c) => c.sections)
            .filter((section) => section.id === openSection)
            .map((section) => (
              <SectionBody key={section.id} section={section} language={language} />
            ))}

          {/*
           * Previous and next, because a guide is READ THROUGH the first time. A contents list
           * alone makes somebody hunt for where they were, and the order the sections are in is
           * the order they build on each other.
           */}
          <div className="flex items-center justify-between gap-2 border-t border-border pt-4">
            <Button
              variant="ghost"
              size="sm"
              disabled={position <= 0}
              onClick={() => setOpenSection(contents[position - 1] ?? FIRST_SECTION)}
            >
              {rtl ? "السابق ←" : "← Previous"}
            </Button>
            <span className="text-xs text-text-muted">
              {position + 1} / {contents.length}
            </span>
            <Button
              variant="ghost"
              size="sm"
              disabled={position >= contents.length - 1}
              onClick={() => setOpenSection(contents[position + 1] ?? FIRST_SECTION)}
            >
              {rtl ? "→ التالي" : "Next →"}
            </Button>
          </div>
        </div>
      </div>
    </section>
  );
}

function SectionBody({ section, language }: { section: Section; language: Language }) {
  const rtl = language === "ar";

  return (
    <article className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h3 className="text-lg font-medium text-text">
          {rtl ? section.titleAr : section.titleEn}
        </h3>
        <p className="text-sm text-text-muted">{rtl ? section.leadAr : section.leadEn}</p>
      </div>

      {section.blocks.map((block, index) => (
        <BlockBody key={index} block={block} language={language} />
      ))}
    </article>
  );
}

function BlockBody({ block, language }: { block: Block; language: Language }) {
  const rtl = language === "ar";

  switch (block.kind) {
    case "text":
      return <p className="text-sm leading-relaxed text-text">{rtl ? block.ar : block.en}</p>;

    case "note":
      return (
        <aside className="rounded-lg border border-border bg-surface-muted p-3 text-sm text-text">
          {rtl ? block.ar : block.en}
        </aside>
      );

    case "steps": {
      const items = rtl ? block.ar : block.en;
      return (
        <ol className="flex flex-col gap-2">
          {items.map((item, index) => (
            <li key={index} className="flex gap-3 text-sm text-text">
              {/* The number is rendered, not a list marker: an RTL list marker sits on the
                  wrong side in several browsers, and the order is the content here. */}
              <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-surface-muted text-xs tabular-nums">
                {index + 1}
              </span>
              <span className="leading-relaxed">{item}</span>
            </li>
          ))}
        </ol>
      );
    }

    case "example":
      return (
        <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-4">
          <h4 className="text-sm font-medium text-text">
            {rtl ? block.titleAr : block.titleEn}
          </h4>
          <ul className="flex flex-col gap-3">
            {block.rows.map((row, index) => (
              <li key={index} className="flex flex-col gap-1">
                <span className="text-sm text-text">
                  <span className="text-text-muted">{rtl ? "تفعل: " : "You: "}</span>
                  {rtl ? row.doAr : row.doEn}
                </span>
                <span className="text-sm text-text-muted">
                  <span>{rtl ? "فيحدث: " : "Mizan: "}</span>
                  {rtl ? row.thenAr : row.thenEn}
                </span>
              </li>
            ))}
          </ul>
        </div>
      );

    case "flow":
      return (
        <ol className="flex flex-col gap-2">
          {block.stages.map((stage, index) => (
            <li
              key={index}
              className="flex items-start gap-3 rounded-lg border border-border bg-surface p-3"
            >
              <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-surface-muted text-xs tabular-nums">
                {index + 1}
              </span>
              <span className="flex flex-1 flex-col gap-1">
                <span className="text-sm text-text">{rtl ? stage.ar : stage.en}</span>
                {/* Which module did it. A reader tracing a figure needs to know where to look
                    next, and "Accounting" is the answer to "why did the balance change". */}
                <Badge tone="neutral">{rtl ? stage.moduleAr : stage.moduleEn}</Badge>
              </span>
            </li>
          ))}
        </ol>
      );
  }
}
