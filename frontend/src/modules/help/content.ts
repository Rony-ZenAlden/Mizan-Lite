/**
 * The in-app guide's content (Step 10.9).
 *
 * # Why the text lives here and not in the i18n catalogue
 *
 * Every other string in this application is a key in `locales/*.json`, because a label belongs
 * with the thousand other labels and a translator works through them as one file.
 *
 * A guide is not labels. It is PROSE — paragraphs that reference each other, ordered steps whose
 * numbering matters, and passages where the English and the Arabic must say the same thing but
 * will not say it the same way. Splitting that across a flat key-value file gives you
 * `help.architecture.para3` and no way to see whether the section still reads.
 *
 * So both languages sit side by side, in one structure, where a change to one is visibly a change
 * that the other needs. `TestEveryHelpSectionExistsInBothLanguages` is what enforces it.
 */

export type Language = "en" | "ar";

/** One block of guide content. Rendered by kind, so the shapes stay small and predictable. */
export type Block =
  | { kind: "text"; en: string; ar: string }
  | { kind: "steps"; en: string[]; ar: string[] }
  | { kind: "note"; en: string; ar: string }
  /** A worked example: what somebody does, and what the system does about it. */
  | { kind: "example"; titleEn: string; titleAr: string; rows: ExampleRow[] }
  /** A flow: each stage, what it produces, and which module owns it. */
  | { kind: "flow"; stages: FlowStage[] };

export interface ExampleRow {
  doEn: string;
  doAr: string;
  thenEn: string;
  thenAr: string;
}

export interface FlowStage {
  en: string;
  ar: string;
  moduleEn: string;
  moduleAr: string;
}

export interface Section {
  id: string;
  titleEn: string;
  titleAr: string;
  /** A one-line summary, shown in the contents list. */
  leadEn: string;
  leadAr: string;
  blocks: Block[];
}

export interface Chapter {
  id: string;
  titleEn: string;
  titleAr: string;
  sections: Section[];
}

export const CHAPTERS: Chapter[] = [
  // ── 1. Getting started ────────────────────────────────────────────────────────
  {
    id: "start",
    titleEn: "Getting started",
    titleAr: "البداية",
    sections: [
      {
        id: "first-run",
        titleEn: "Your first hour",
        titleAr: "ساعتك الأولى",
        leadEn: "Set the business up once, then put in what you sell.",
        leadAr: "أعدّ النشاط مرة واحدة، ثم أدخل ما تبيعه.",
        blocks: [
          {
            kind: "text",
            en: "Mizan asks for a few things the first time it opens. Take your time — the currency and the financial year are difficult to change afterwards, and the rest is not.",
            ar: "يسألك ميزان عن بضعة أمور في أول فتح. خذ وقتك — العملة والسنة المالية يصعب تغييرهما لاحقاً، وما عداهما لا.",
          },
          {
            kind: "steps",
            en: [
              "Name your business, and choose its country and currency. The country decides which chart of accounts and tax rules you start with.",
              "Name your first branch and warehouse — for one shop, that is the shop and its store room.",
              "Choose the month your financial year starts. Ask whoever files your tax return if you are unsure.",
              "Create your own account. It can do everything, so keep the password safe: there is nobody to reset it for you.",
            ],
            ar: [
              "سمِّ نشاطك، واختر دولته وعملته. الدولة تحدّد دليل الحسابات وقواعد الضريبة التي تبدأ بها.",
              "سمِّ أول فرع ومستودع — للمتجر الواحد، هما المتجر ومخزنه.",
              "اختر الشهر الذي تبدأ فيه سنتك المالية. اسأل من يقدّم إقرارك الضريبي إن لم تكن متأكداً.",
              "أنشئ حسابك. هذا الحساب يستطيع كل شيء، فاحفظ كلمة المرور: لا يوجد من يعيد ضبطها لك.",
            ],
          },
          {
            kind: "note",
            en: "Everything after this is optional until you need it. A sale to a walk-in customer needs no customer record at all.",
            ar: "كل ما بعد ذلك اختياري حتى تحتاجه. البيع لعميل عابر لا يحتاج سجل عميل أصلاً.",
          },
          {
            kind: "steps",
            en: [
              "Units — pieces, kilograms, litres. A standard set is already there; add what you use.",
              "Products — what you sell. Each needs a code and a name. A spreadsheet can be imported from Operations → Import.",
              "Stock — how much you have. Either receive it against a purchase, or record an opening count.",
            ],
            ar: [
              "الوحدات — قطعة، كيلوغرام، لتر. المجموعة القياسية موجودة؛ أضف ما تستخدمه.",
              "الأصناف — ما تبيعه. لكل صنف رمز واسم. يمكن استيراد ملف من التشغيل ← الاستيراد.",
              "المخزون — كم لديك. إما باستلامه على أمر شراء، أو بتسجيل جرد افتتاحي.",
            ],
          },
        ],
      },
      {
        id: "daily",
        titleEn: "A day in the shop",
        titleAr: "يوم في المتجر",
        leadEn: "The four screens most days are spent on.",
        leadAr: "الشاشات الأربع التي تمضي فيها معظم الأيام.",
        blocks: [
          {
            kind: "example",
            titleEn: "Selling",
            titleAr: "البيع",
            rows: [
              {
                doEn: "Open the Till and scan or pick items",
                doAr: "افتح الكاشير وامسح الأصناف أو اخترها",
                thenEn: "Prices come from the price list; tax is worked out per line",
                thenAr: "تأتي الأسعار من قائمة الأسعار، وتُحسب الضريبة لكل سطر",
              },
              {
                doEn: "Take payment and post the sale",
                doAr: "استلم الدفعة ورحّل البيع",
                thenEn: "Stock leaves, the books are written, and the invoice is numbered — in one step that either fully happens or does not happen at all",
                thenAr: "يخرج المخزون، وتُكتب الدفاتر، وتُرقّم الفاتورة — في خطوة واحدة تتم كاملة أو لا تتم",
              },
              {
                doEn: "Print or reprint the receipt",
                doAr: "اطبع الفاتورة أو أعد طباعتها",
                thenEn: "A reprint two years later shows exactly what was agreed, even after prices and tax rates have changed",
                thenAr: "إعادة الطباعة بعد سنتين تُظهر ما اتُّفق عليه بالضبط، حتى بعد تغيّر الأسعار والضرائب",
              },
            ],
          },
          {
            kind: "example",
            titleEn: "Buying",
            titleAr: "الشراء",
            rows: [
              {
                doEn: "Raise a purchase order",
                doAr: "أنشئ أمر شراء",
                thenEn: "Nothing moves and nothing is owed — an order is an intention",
                thenAr: "لا شيء يتحرّك ولا شيء يُستحق — الأمر نيّة",
              },
              {
                doEn: "Record the delivery when goods arrive",
                doAr: "سجّل الاستلام عند وصول البضاعة",
                thenEn: "Stock rises and the amount owed is accrued, even though no invoice has come",
                thenAr: "يرتفع المخزون ويُستحق المبلغ، رغم أن الفاتورة لم تصل",
              },
              {
                doEn: "Enter the supplier's invoice",
                doAr: "أدخل فاتورة المورّد",
                thenEn: "The accrual clears exactly; any price difference is booked against the stock still on hand",
                thenAr: "يُقفل الاستحقاق بالضبط، ويُقيَّد أي فرق سعر على المخزون المتبقي",
              },
            ],
          },
          {
            kind: "note",
            en: "Needs attention is worth a look each morning. It only shows conditions that are true right now, and they disappear on their own when you fix them.",
            ar: "يستحق «يحتاج انتباهاً» نظرة كل صباح. لا يعرض إلا حالات قائمة الآن، وتختفي وحدها حين تعالجها.",
          },
        ],
      },
    ],
  },

  // ── 2. How it is built ────────────────────────────────────────────────────────
  {
    id: "architecture",
    titleEn: "How Mizan is built",
    titleAr: "كيف بُني ميزان",
    sections: [
      {
        id: "modules",
        titleEn: "Modules, and why they cannot see each other",
        titleAr: "الوحدات، ولماذا لا ترى بعضها",
        leadEn: "Fifteen parts, each owning its own data.",
        leadAr: "خمس عشرة وحدة، كل منها تملك بياناتها.",
        blocks: [
          {
            kind: "text",
            en: "Mizan is one application made of fifteen modules — sales, inventory, accounting, purchasing and so on. Each owns its own tables and no module may read another's directly.",
            ar: "ميزان تطبيق واحد مكوّن من خمس عشرة وحدة — المبيعات، المخزون، المحاسبة، المشتريات وغيرها. كل وحدة تملك جداولها، ولا يجوز لوحدة أن تقرأ جداول أخرى مباشرة.",
          },
          {
            kind: "text",
            en: "When sales needs to know what stock costs, it does not query the inventory tables. It asks through a narrow, named connection, and the application wires the two together at startup. This is why a change to how stock is costed cannot silently break the till.",
            ar: "حين تحتاج المبيعات معرفة تكلفة المخزون، لا تستعلم من جداول المخزون. تسأل عبر وصلة ضيّقة مسمّاة، والتطبيق يربط الاثنتين عند الإقلاع. لهذا لا يمكن لتغيير في طريقة تكلفة المخزون أن يعطّل الكاشير بصمت.",
          },
          {
            kind: "note",
            en: "The rule is enforced by tooling, not by review. A build that breaks it fails before it runs.",
            ar: "القاعدة تفرضها الأدوات لا المراجعة. البناء الذي يخالفها يفشل قبل أن يعمل.",
          },
        ],
      },
      {
        id: "ledger",
        titleEn: "Every figure comes from the books",
        titleAr: "كل رقم يأتي من الدفاتر",
        leadEn: "No module decides which account money lands in.",
        leadAr: "لا وحدة تقرّر في أي حساب يقع المال.",
        blocks: [
          {
            kind: "text",
            en: "Selling something does not write to the accounts directly. It announces what happened — a sale, paid in cash, of these goods at this cost — and a table of posting rules decides which accounts move.",
            ar: "بيع شيء لا يكتب في الحسابات مباشرة. بل يُعلن ما حدث — بيع، دفع نقدي، لهذه البضاعة بهذه التكلفة — وجدول قواعد الترحيل يقرّر أي الحسابات تتحرّك.",
          },
          {
            kind: "text",
            en: "That is why a country that books things differently is a configuration change and not a new version of the program, and why no part of the sales code contains an account number.",
            ar: "لهذا فإن دولة تقيّد الأمور بطريقة مختلفة تعني تغييراً في الإعداد لا نسخة جديدة من البرنامج، ولهذا لا يحتوي أي جزء من كود المبيعات على رقم حساب.",
          },
          {
            kind: "note",
            en: "The books are double-entry and always balanced. An entry whose debits and credits differ cannot be created at all — not rejected when saved, but impossible to build.",
            ar: "الدفاتر مزدوجة القيد ومتوازنة دائماً. القيد الذي تختلف مدينته عن دائنته لا يمكن إنشاؤه أصلاً — ليس مرفوضاً عند الحفظ، بل مستحيل التكوين.",
          },
        ],
      },
      {
        id: "money",
        titleEn: "Money is never a decimal",
        titleAr: "المال ليس كسراً عشرياً أبداً",
        leadEn: "Whole units, so nothing is ever a hundredth out.",
        leadAr: "وحدات صحيحة، فلا يختلّ شيء بجزء من مئة.",
        blocks: [
          {
            kind: "text",
            en: "Every amount is stored as a whole number of the smallest unit — fils, halalas, cents. A decimal fraction cannot represent a tenth exactly, and an ERP that adds thousands of them ends the month a few units out with nobody able to say where.",
            ar: "يُخزَّن كل مبلغ كعدد صحيح من أصغر وحدة — فلس، هللة، سنت. الكسر العشري لا يمثّل العُشر بدقة، ونظام يجمع آلافاً منها ينهي الشهر بفارق وحدات لا يعرف أحد مصدره.",
          },
          {
            kind: "text",
            en: "Quantities work the same way, to six decimal places, so 1/3 of a kilogram behaves consistently everywhere it appears.",
            ar: "الكميات كذلك، بستّ منازل عشرية، فيتصرّف ثلث الكيلوغرام بالطريقة نفسها أينما ظهر.",
          },
        ],
      },
    ],
  },

  // ── 3. Where data goes ────────────────────────────────────────────────────────
  {
    id: "data",
    titleEn: "Where your data goes",
    titleAr: "إلى أين تذهب بياناتك",
    sections: [
      {
        id: "flow",
        titleEn: "One sale, end to end",
        titleAr: "بيعة واحدة، من البداية للنهاية",
        leadEn: "What happens between pressing Post and the receipt printing.",
        leadAr: "ما يحدث بين ضغط «ترحيل» وطباعة الفاتورة.",
        blocks: [
          {
            kind: "flow",
            stages: [
              {
                en: "The price is resolved for this customer and quantity",
                ar: "يُحدَّد السعر لهذا العميل وهذه الكمية",
                moduleEn: "Pricing",
                moduleAr: "التسعير",
              },
              {
                en: "Tax is worked out per line, at the rate in force on the day",
                ar: "تُحسب الضريبة لكل سطر، بالنسبة السارية في ذلك اليوم",
                moduleEn: "Tax",
                moduleAr: "الضريبة",
              },
              {
                en: "Stock leaves, and what it cost is frozen onto the line",
                ar: "يخرج المخزون، وتُثبَّت تكلفته على السطر",
                moduleEn: "Inventory",
                moduleAr: "المخزون",
              },
              {
                en: "The invoice takes the next number in its series",
                ar: "تأخذ الفاتورة الرقم التالي في تسلسلها",
                moduleEn: "Numbering",
                moduleAr: "الترقيم",
              },
              {
                en: "Posting rules turn the sale into journal entries",
                ar: "تحوّل قواعد الترحيل البيع إلى قيود",
                moduleEn: "Accounting",
                moduleAr: "المحاسبة",
              },
              {
                en: "The act is recorded, with who did it and when",
                ar: "يُسجَّل الإجراء، بمن قام به ومتى",
                moduleEn: "Audit",
                moduleAr: "التدقيق",
              },
            ],
          },
          {
            kind: "note",
            en: "All of it happens in one transaction. If any step fails, none of it happened — the stock is not gone, the number is not used, and the books are untouched.",
            ar: "كل ذلك يحدث في معاملة واحدة. إن فشلت أي خطوة، لم يحدث شيء — لم يخرج المخزون، ولم يُستهلك الرقم، ولم تُمسّ الدفاتر.",
          },
        ],
      },
      {
        id: "storage",
        titleEn: "Your data lives on this computer",
        titleAr: "بياناتك على هذا الجهاز",
        leadEn: "One file, no internet, and a daily backup.",
        leadAr: "ملف واحد، بلا إنترنت، ونسخة يومية.",
        blocks: [
          {
            kind: "text",
            en: "Everything is in one database file on this machine. Mizan is not a website and never sends your data anywhere. It works with the internet switched off, permanently.",
            ar: "كل شيء في ملف قاعدة بيانات واحد على هذا الجهاز. ميزان ليس موقعاً ولا يرسل بياناتك إلى أي مكان. يعمل والإنترنت مغلق، دائماً.",
          },
          {
            kind: "steps",
            en: [
              "A backup is taken every day, automatically.",
              "Each backup is opened and checked before it is kept — an unverified backup is a rumour.",
              "The last seven of each kind are kept; older ones are removed.",
              "A backup is also taken before any update, and before any restore.",
            ],
            ar: [
              "تُؤخذ نسخة احتياطية كل يوم، تلقائياً.",
              "تُفتح كل نسخة ويُتحقّق منها قبل الاحتفاظ بها — النسخة غير المتحقّقة إشاعة.",
              "يُحتفظ بآخر سبع من كل نوع، وتُحذف الأقدم.",
              "وتُؤخذ نسخة قبل أي تحديث، وقبل أي استعادة.",
            ],
          },
          {
            kind: "note",
            en: "Copy a backup off this computer once a week. It is the one thing Mizan cannot do for you: a backup on the same disk does not survive that disk failing.",
            ar: "انسخ نسخة احتياطية خارج هذا الجهاز مرة كل أسبوع. هذا هو الشيء الوحيد الذي لا يستطيع ميزان فعله عنك: النسخة على القرص نفسه لا تنجو من تعطّله.",
          },
        ],
      },
    ],
  },

  // ── 4. Building and shipping ──────────────────────────────────────────────────
  {
    id: "build",
    titleEn: "How Mizan is built and shipped",
    titleAr: "كيف يُبنى ميزان ويُوزَّع",
    sections: [
      {
        id: "pipeline",
        titleEn: "From source to installer",
        titleAr: "من الشيفرة إلى المثبّت",
        leadEn: "Every check runs on the build machine, offline.",
        leadAr: "كل الفحوصات تجري على جهاز البناء، دون إنترنت.",
        blocks: [
          {
            kind: "flow",
            stages: [
              {
                en: "Every test runs, with the race detector and coverage",
                ar: "تعمل كل الاختبارات، مع كاشف التسابق وقياس التغطية",
                moduleEn: "make ci",
                moduleAr: "make ci",
              },
              {
                en: "Architecture rules are enforced — module isolation, no floating-point money",
                ar: "تُفرض قواعد المعمارية — عزل الوحدات، ولا مال بفاصلة عائمة",
                moduleEn: "archlint",
                moduleAr: "archlint",
              },
              {
                en: "Every error code is checked to have a translation in both languages",
                ar: "يُتحقّق أن لكل رمز خطأ ترجمة باللغتين",
                moduleEn: "i18n gate",
                moduleAr: "بوابة الترجمة",
              },
              {
                en: "The frontend is compiled and bundled into the binary",
                ar: "تُبنى الواجهة وتُدمج داخل الملف التنفيذي",
                moduleEn: "Vite",
                moduleAr: "Vite",
              },
              {
                en: "One binary per platform, with the version stamped in",
                ar: "ملف تنفيذي لكل نظام، مختوم برقم الإصدار",
                moduleEn: "Wails",
                moduleAr: "Wails",
              },
              {
                en: "An installer is wrapped around it: .dmg or Setup.exe",
                ar: "يُغلَّف بمثبّت: ‎.dmg أو Setup.exe",
                moduleEn: "hdiutil / NSIS",
                moduleAr: "hdiutil / NSIS",
              },
            ],
          },
          {
            kind: "text",
            en: "There is no build server. Everything runs on one machine with no internet connection, which is the same constraint the product itself is built for.",
            ar: "لا يوجد خادم بناء. كل شيء يجري على جهاز واحد بلا اتصال بالإنترنت، وهو القيد نفسه الذي بُني المنتج لأجله.",
          },
        ],
      },
      {
        id: "updating",
        titleEn: "Updating safely",
        titleAr: "التحديث بأمان",
        leadEn: "What happens when you install a newer version.",
        leadAr: "ما يحدث حين تثبّت إصداراً أحدث.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Install the new version over the old one. Your data is not touched by the installer.",
              "On first launch, Mizan checks the database is healthy before changing anything.",
              "It takes a backup and verifies it can be opened.",
              "It applies whatever schema changes the new version needs, in order, in one transaction.",
              "If any of that fails, the database is left exactly as it was and the backup's location is shown on screen.",
            ],
            ar: [
              "ثبّت الإصدار الجديد فوق القديم. لا يمسّ المثبّت بياناتك.",
              "عند أول تشغيل، يتحقّق ميزان من سلامة قاعدة البيانات قبل تغيير أي شيء.",
              "يأخذ نسخة احتياطية ويتأكّد أنها تُفتح.",
              "يطبّق تغييرات البنية التي يحتاجها الإصدار الجديد، بالترتيب، في معاملة واحدة.",
              "إن فشل أي من ذلك، تبقى قاعدة البيانات كما كانت تماماً ويُعرض مكان النسخة الاحتياطية على الشاشة.",
            ],
          },
          {
            kind: "note",
            en: "A version older than your data will refuse to open it rather than damage it. Restoring a backup taken by a newer version is refused for the same reason.",
            ar: "الإصدار الأقدم من بياناتك يرفض فتحها بدل إتلافها. واستعادة نسخة أُخذت بإصدار أحدث مرفوضة للسبب نفسه.",
          },
        ],
      },
    ],
  },
];

/**
 * The section a reader lands on.
 *
 * Derived rather than written down, so reordering the guide cannot leave the screen pointing at a
 * section that no longer exists — which would render an empty page with working navigation, the
 * kind of break nobody notices until somebody opens the guide.
 */
export const FIRST_SECTION: string = CHAPTERS[0]?.sections[0]?.id ?? "";

/** Every section id, in reading order. The guide is read through the first time. */
export const READING_ORDER: string[] = CHAPTERS.flatMap((chapter) =>
  chapter.sections.map((section) => section.id),
);
