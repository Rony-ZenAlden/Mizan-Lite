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

  // ── 2. Using each part ────────────────────────────────────────────────────────
  {
    id: "using",
    titleEn: "Using each part",
    titleAr: "استخدام كل جزء",
    sections: [
      {
        id: "products",
        titleEn: "Products and the catalogue",
        titleAr: "الأصناف والدليل",
        leadEn: "Register what you sell, once.",
        leadAr: "سجّل ما تبيعه، مرة واحدة.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Open Catalogue from the menu.",
              "Press New product.",
              "Type a code — the short reference you use, such as OIL-1L. It must be unique, and Mizan refuses a duplicate rather than creating a second product nobody can tell apart.",
              "Type the name as a customer would read it on a receipt.",
              "Choose the unit: pieces, kilograms, litres. If you sell it one at a time, pieces is right.",
              "Choose a category if you have one. You can leave this empty and file it later.",
              "Save. The product is ready to sell immediately.",
            ],
            ar: [
              "افتح «الدليل» من القائمة.",
              "اضغط «صنف جديد».",
              "اكتب رمزاً — الاختصار الذي تستخدمه، مثل OIL-1L. يجب أن يكون فريداً، ويرفض ميزان المكرر بدل إنشاء صنف ثانٍ لا يمكن تمييزه.",
              "اكتب الاسم كما يقرؤه العميل على الفاتورة.",
              "اختر الوحدة: قطعة، كيلوغرام، لتر. إن كنت تبيعه واحداً واحداً فالقطعة هي الصحيحة.",
              "اختر التصنيف إن كان لديك. يمكنك تركه فارغاً وتصنيفه لاحقاً.",
              "احفظ. الصنف جاهز للبيع فوراً.",
            ],
          },
          {
            kind: "note",
            en: "Adding many products at once? Operations → Import takes a spreadsheet with code and name columns, checks every row before writing anything, and tells you exactly which rows would fail.",
            ar: "تضيف أصنافاً كثيرة دفعة واحدة؟ «التشغيل ← الاستيراد» يقبل ملفاً فيه عمودا الرمز والاسم، ويفحص كل صف قبل كتابة أي شيء، ويخبرك بالضبط أي الصفوف سيفشل.",
          },
          {
            kind: "note",
            en: "Every product gets one variant automatically. You never see it unless you sell the same product in several sizes or colours — then each size is a variant with its own stock and its own price.",
            ar: "كل صنف يحصل على متغيّر واحد تلقائياً. لن تراه أبداً إلا إذا كنت تبيع الصنف نفسه بعدة أحجام أو ألوان — عندها يكون كل حجم متغيّراً له مخزونه وسعره.",
          },
        ],
      },
      {
        id: "stock",
        titleEn: "Stock and counting",
        titleAr: "المخزون والجرد",
        leadEn: "What is on the shelf, and correcting it when it is not.",
        leadAr: "ما هو على الرف، وتصحيحه حين لا يكون كذلك.",
        blocks: [
          {
            kind: "text",
            en: "Stock changes on its own as you trade: a sale takes it out, a delivery puts it in. You only touch it directly when the shelf and the screen disagree.",
            ar: "يتغيّر المخزون وحده مع التداول: البيع يُخرجه، والاستلام يُدخله. لا تلمسه مباشرة إلا حين يختلف الرف عن الشاشة.",
          },
          {
            kind: "steps",
            en: [
              "Open Stock from the menu. Each row is a product in a warehouse, with what Mizan believes is there.",
              "Find the product whose count is wrong.",
              "Press Count and type what is ACTUALLY on the shelf — not the difference.",
              "Give a short reason: damaged, miscounted, expired. This is what an auditor reads later.",
              "Save. Mizan works out the difference and records a movement for it.",
            ],
            ar: [
              "افتح «المخزون» من القائمة. كل سطر صنف في مستودع، ومعه ما يعتقد ميزان أنه موجود.",
              "ابحث عن الصنف الذي عدده خاطئ.",
              "اضغط «جرد» واكتب ما هو موجود فعلاً على الرف — لا الفرق.",
              "اذكر سبباً مختصراً: تالف، خطأ في العدّ، منتهي الصلاحية. هذا ما يقرؤه المدقّق لاحقاً.",
              "احفظ. يحسب ميزان الفرق ويسجّل حركة به.",
            ],
          },
          {
            kind: "note",
            en: "You type what is there, not the difference — because working out that eleven is two fewer than thirteen is arithmetic the computer should do, and the subtraction a tired person gets wrong at the end of a long day.",
            ar: "تكتب ما هو موجود لا الفرق — لأن حساب أن أحد عشر أقل باثنين من ثلاثة عشر عملية يجب أن يقوم بها الحاسوب، وهي الطرح الذي يخطئ فيه المتعب في آخر يوم طويل.",
          },
          {
            kind: "note",
            en: "A stock adjustment needs its own permission. The person who can see stock and the person who can change what the system believes about it are different people in any business large enough to have both.",
            ar: "تعديل المخزون يحتاج صلاحية خاصة. من يرى المخزون ومن يغيّر اعتقاد النظام عنه شخصان مختلفان في أي نشاط كبير بما يكفي ليضمّ الاثنين.",
          },
        ],
      },
      {
        id: "selling",
        titleEn: "Selling at the till",
        titleAr: "البيع على الكاشير",
        leadEn: "Open a shift, sell, take payment, close.",
        leadAr: "افتح وردية، بِع، استلم، أغلق.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Open the Till. If no shift is open, open one and enter the cash you are starting with.",
              "Scan a barcode or search for the product. It goes on the sale with its price already worked out.",
              "Change the quantity if you need to; the line total follows.",
              "Add a customer only if they want an invoice in their name. Most sales need none.",
              "Press Pay. Choose cash, card, or transfer, and enter the amount.",
              "Post the sale. Stock leaves, the books are written, and the invoice takes its number — all at once.",
              "Print the receipt, or reprint it later from Invoices.",
            ],
            ar: [
              "افتح «الكاشير». إن لم تكن هناك وردية مفتوحة، افتح واحدة وأدخل النقد الذي تبدأ به.",
              "امسح الباركود أو ابحث عن الصنف. يُضاف إلى البيع بسعره محسوباً.",
              "غيّر الكمية إن احتجت؛ يتبعها إجمالي السطر.",
              "أضف عميلاً فقط إن أراد فاتورة باسمه. معظم المبيعات لا تحتاج ذلك.",
              "اضغط «دفع». اختر نقداً أو بطاقة أو حوالة، وأدخل المبلغ.",
              "رحّل البيع. يخرج المخزون وتُكتب الدفاتر وتأخذ الفاتورة رقمها — كل ذلك دفعة واحدة.",
              "اطبع الفاتورة، أو أعد طباعتها لاحقاً من «الفواتير».",
            ],
          },
          {
            kind: "steps",
            en: [
              "At the end of the day, press Close shift.",
              "Count the cash drawer and enter what is actually in it.",
              "Mizan shows the difference against what it expected. A difference is recorded, not hidden — that is what the count is for.",
            ],
            ar: [
              "في نهاية اليوم، اضغط «إغلاق الوردية».",
              "عُدّ الدرج وأدخل ما فيه فعلاً.",
              "يعرض ميزان الفرق عمّا توقّعه. الفرق يُسجَّل ولا يُخفى — وهذا هو الغرض من العدّ.",
            ],
          },
          {
            kind: "note",
            en: "A customer returning something: open the invoice from Invoices and press Return. Choose what comes back. The goods return to stock at what they originally cost, and the customer is credited what they were charged — two different figures, both correct.",
            ar: "عميل يُرجع شيئاً: افتح الفاتورة من «الفواتير» واضغط «إرجاع». اختر ما يعود. تعود البضاعة إلى المخزون بتكلفتها الأصلية، ويُمنح العميل ما دُفع — رقمان مختلفان وكلاهما صحيح.",
          },
        ],
      },
      {
        id: "buying",
        titleEn: "Buying from suppliers",
        titleAr: "الشراء من الموردين",
        leadEn: "Order, receive, get invoiced, pay.",
        leadAr: "اطلب، استلم، افوتر، ادفع.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Purchases → Orders → New order. Choose the supplier and add what you want.",
              "Place the order. Nothing has moved and nothing is owed — an order is an intention.",
              "When the goods arrive, open Deliveries and record what actually came. It may be less than ordered, and that is normal.",
              "Stock rises immediately. The amount owed is set aside even though no invoice has arrived.",
              "When the supplier's invoice comes, open Bills → New bill, pick the deliveries it covers, and enter their invoice number.",
              "Post the bill. What was set aside clears exactly. If they charged a different price, the difference goes onto the stock still on hand.",
              "Pay from Supplier payments when you settle up.",
            ],
            ar: [
              "«المشتريات ← الأوامر ← أمر جديد». اختر المورّد وأضف ما تريد.",
              "أرسل الأمر. لم يتحرّك شيء ولم يُستحق شيء — الأمر نيّة.",
              "عند وصول البضاعة، افتح «الاستلامات» وسجّل ما وصل فعلاً. قد يكون أقل من المطلوب، وهذا طبيعي.",
              "يرتفع المخزون فوراً. ويُخصَّص المبلغ المستحق رغم عدم وصول الفاتورة.",
              "عند وصول فاتورة المورّد، افتح «الفواتير ← فاتورة جديدة»، اختر الاستلامات التي تغطيها، وأدخل رقم فاتورتهم.",
              "رحّل الفاتورة. يُقفل المخصَّص بالضبط. وإن كان السعر مختلفاً، يذهب الفرق على المخزون المتبقي.",
              "ادفع من «مدفوعات الموردين» عند التسوية.",
            ],
          },
          {
            kind: "note",
            en: "Freight, customs or clearing charges: open the delivery and add them under Charges. Recording a charge and putting it into the cost of the goods are two separate presses, because the second changes what every future sale of those goods is measured against.",
            ar: "الشحن أو الجمارك أو التخليص: افتح الاستلام وأضفها تحت «الرسوم». تسجيل الرسم وإضافته إلى تكلفة البضاعة ضغطتان منفصلتان، لأن الثانية تغيّر ما تُقاس عليه كل بيعة مستقبلية لتلك البضاعة.",
          },
          {
            kind: "note",
            en: "Sending goods back: Purchases → Returns to suppliers. The supplier credits what they charged; the stock leaves at what that delivery cost you.",
            ar: "إرجاع بضاعة: «المشتريات ← المرتجعات للموردين». يمنحك المورّد إشعاراً بما حمّلك، ويخرج المخزون بما كلّفك ذلك الاستلام.",
          },
        ],
      },
      {
        id: "people",
        titleEn: "Customers and suppliers",
        titleAr: "العملاء والموردون",
        leadEn: "Only the ones you need a record of.",
        leadAr: "فقط من تحتاج سجلاً لهم.",
        blocks: [
          {
            kind: "text",
            en: "A walk-in sale needs no customer at all. Add one when you sell on credit, when they want an invoice in their name, or when you want to know what they buy.",
            ar: "البيع لعميل عابر لا يحتاج سجلاً أصلاً. أضف عميلاً حين تبيع بالآجل، أو حين يريد فاتورة باسمه، أو حين تريد معرفة ما يشتريه.",
          },
          {
            kind: "steps",
            en: [
              "Customers → New. A code and a name are enough.",
              "Add a phone number if you have one — it is how you will find them again, faster than by name.",
              "Set a credit limit only if you sell to them on account. Mizan refuses a sale that would take them over it.",
              "Payment terms decide when their invoices become overdue, which is what the ageing report is built on.",
            ],
            ar: [
              "«العملاء ← جديد». الرمز والاسم يكفيان.",
              "أضف رقم هاتف إن وُجد — هو ما ستجدهم به لاحقاً، أسرع من الاسم.",
              "ضع حدّ ائتمان فقط إن كنت تبيع لهم بالآجل. يرفض ميزان بيعة تتجاوزه.",
              "شروط السداد تحدّد متى تصبح فواتيرهم متأخّرة، وعليها يُبنى تقرير الأعمار.",
            ],
          },
          {
            kind: "note",
            en: "One person can be both a customer and a supplier. Mizan keeps the two sides apart: what they owe you and what you owe them are shown separately, because netting them hides which one is at risk.",
            ar: "قد يكون الشخص عميلاً ومورّداً معاً. يفصل ميزان بين الجانبين: ما لك عليهم وما لهم عليك يُعرضان منفصلين، لأن دمجهما يُخفي أيّهما في خطر.",
          },
        ],
      },
      {
        id: "spending",
        titleEn: "Expenses and debts",
        titleAr: "المصروفات والديون",
        leadEn: "Rent, wages, and money that is not a purchase.",
        leadAr: "الإيجار والأجور والمال الذي ليس شراءً.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Money → Expenses → New. Choose a category — rent, wages, utilities. The category decides which account it lands in, so you never pick an account.",
              "Enter the amount and who it was paid to.",
              "Say whether you paid it now or owe it. Paid now takes the money; owed adds it to what you owe and appears on their statement.",
              "Save. The books are written.",
            ],
            ar: [
              "«المال ← المصروفات ← جديد». اختر تصنيفاً — إيجار، أجور، مرافق. التصنيف يحدّد الحساب الذي يقع فيه، فلا تختار حساباً أبداً.",
              "أدخل المبلغ ولمن دُفع.",
              "حدّد إن كنت دفعته الآن أم أنه مستحق. المدفوع الآن يأخذ المال، والمستحق يُضاف إلى ما عليك ويظهر في كشفهم.",
              "احفظ. تُكتب الدفاتر.",
            ],
          },
          {
            kind: "note",
            en: "Debts are different from expenses: a loan taken or repaid, or the owner putting money in or taking it out. They move money without being a cost, so Money → Debts keeps them apart from anything that affects profit.",
            ar: "الديون تختلف عن المصروفات: قرض أُخذ أو سُدِّد، أو مال أدخله المالك أو أخرجه. تحرّك المال دون أن تكون تكلفة، لذا يفصلها «المال ← الديون» عن كل ما يمسّ الربح.",
          },
        ],
      },
      {
        id: "reports",
        titleEn: "Reports",
        titleAr: "التقارير",
        leadEn: "What happened, and where you stand.",
        leadAr: "ما حدث، وأين تقف.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Reports → Financial statements shows the profit and loss for a period, and the balance sheet at its end. Pick the dates at the top.",
              "Reports → Sales & spend answers which products and which customers make money. Switch between sales and spend, and group by day, product, or partner.",
              "Reports → Stock valuation shows what is on the shelf and what it is worth — and whether the accounts agree with it.",
              "Any report can be exported to a spreadsheet with the button on the screen.",
            ],
            ar: [
              "«التقارير ← القوائم المالية» تعرض الأرباح والخسائر لفترة، والميزانية في نهايتها. اختر التواريخ في الأعلى.",
              "«التقارير ← المبيعات والمشتريات» يجيب أي الأصناف وأي العملاء يربحون. بدّل بين المبيعات والمشتريات، وجمّع حسب اليوم أو الصنف أو الشريك.",
              "«التقارير ← تقييم المخزون» يعرض ما على الرف وقيمته — وهل توافقه الحسابات.",
              "يمكن تصدير أي تقرير إلى ملف من الزر الموجود على الشاشة.",
            ],
          },
          {
            kind: "note",
            en: "Gross margin and net profit are different numbers and both are shown. Gross margin is what you made on the goods; net profit is that less rent, wages and everything else. Neither is labelled just 'profit', because reading one as the other is how a shop concludes it is doing well while losing money.",
            ar: "مجمل الربح وصافي الربح رقمان مختلفان وكلاهما معروض. مجمل الربح ما ربحته من البضاعة؛ وصافي الربح هو ذلك ناقص الإيجار والأجور وكل ما عداها. ولا يُسمّى أيٌّ منهما «ربحاً» فقط، لأن قراءة أحدهما مكان الآخر هي كيف يستنتج متجر أنه بخير وهو يخسر.",
          },
        ],
      },
      {
        id: "operations",
        titleEn: "Backups, import, and looking after it",
        titleAr: "النسخ الاحتياطية والاستيراد والعناية",
        leadEn: "The screens you use rarely and need to work.",
        leadAr: "الشاشات التي تستخدمها نادراً ويجب أن تعمل.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Operations → Backups lists every copy Mizan has taken. One is taken daily, automatically, and checked before it is kept.",
              "Press Back up now before anything risky — a large import, a big stock count.",
              "To go back to an earlier copy, press Restore beside it. Mizan copies your current data first, then asks you to close and reopen.",
              "Copy a backup onto a USB stick once a week. This is the one thing Mizan cannot do for you.",
            ],
            ar: [
              "«التشغيل ← النسخ الاحتياطية» يعرض كل نسخة أخذها ميزان. تُؤخذ واحدة يومياً تلقائياً، ويُتحقّق منها قبل الاحتفاظ بها.",
              "اضغط «انسخ الآن» قبل أي أمر محفوف — استيراد كبير، جرد كبير.",
              "للعودة إلى نسخة أقدم، اضغط «استعادة» بجانبها. ينسخ ميزان بياناتك الحالية أولاً، ثم يطلب إغلاق البرنامج وفتحه.",
              "انسخ نسخة احتياطية على ذاكرة USB مرة كل أسبوع. هذا هو الشيء الوحيد الذي لا يستطيع ميزان فعله عنك.",
            ],
          },
          {
            kind: "steps",
            en: [
              "Operations → Import brings products or customers in from a spreadsheet.",
              "Choose what you are importing and pick the file.",
              "Press Check the file. Nothing is written — you see exactly what would go in and what would fail, row by row.",
              "Only then does the Import button appear.",
            ],
            ar: [
              "«التشغيل ← الاستيراد» يُدخل الأصناف أو العملاء من ملف.",
              "اختر ما تستورده واختر الملف.",
              "اضغط «افحص الملف». لا يُكتب شيء — ترى بالضبط ما سيدخل وما سيفشل، صفاً صفاً.",
              "عندها فقط يظهر زر الاستيراد.",
            ],
          },
          {
            kind: "note",
            en: "Needs attention is where Mizan tells you about itself: books that do not balance, stock the accounts disagree with, a failed daily task, or no backup for days. Each disappears on its own when you fix the cause.",
            ar: "«يحتاج انتباهاً» هو حيث يخبرك ميزان عن نفسه: دفاتر غير متوازنة، مخزون تخالفه الحسابات، مهمة يومية فشلت، أو غياب نسخة احتياطية لأيام. يختفي كل تنبيه وحده حين تعالج سببه.",
          },
        ],
      },
      {
        id: "settings",
        titleEn: "People and settings",
        titleAr: "المستخدمون والإعدادات",
        leadEn: "Who can do what.",
        leadAr: "من يستطيع أن يفعل ماذا.",
        blocks: [
          {
            kind: "steps",
            en: [
              "Users lists everybody who can sign in. Add one per person — a shared account tells you nothing about who did what.",
              "Roles decide what each person can reach. A till operator needs to sell; they do not need to adjust stock or read margins.",
              "A till operator can be given a PIN, so they sign in quickly at the counter without a password.",
              "Sessions shows who is signed in now, and lets you end a session on a machine somebody walked away from.",
              "Audit records every change: who, what, when, and what it was before.",
            ],
            ar: [
              "«المستخدمون» يعرض كل من يمكنه الدخول. أضف واحداً لكل شخص — الحساب المشترك لا يخبرك من فعل ماذا.",
              "«الأدوار» تحدّد ما يصل إليه كل شخص. عامل الكاشير يحتاج أن يبيع؛ ولا يحتاج تعديل المخزون ولا قراءة الهوامش.",
              "يمكن إعطاء عامل الكاشير رمزاً سرياً ليدخل بسرعة عند الطاولة دون كلمة مرور.",
              "«الجلسات» يعرض من هو داخل الآن، ويتيح إنهاء جلسة على جهاز تركه أحدهم.",
              "«التدقيق» يسجّل كل تغيير: من ومتى وماذا وما كان قبله.",
            ],
          },
          {
            kind: "note",
            en: "Change your own password from Account → Password. This needs no permission at all: the person who most needs it may hold nothing else.",
            ar: "غيّر كلمة مرورك من «الحساب ← كلمة المرور». لا يحتاج ذلك أي صلاحية: من يحتاجه أكثر قد لا يملك شيئاً غيره.",
          },
        ],
      },
    ],
  },

  // ── 3. How it is built ────────────────────────────────────────────────────────
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

  // ── 4. Where data goes ────────────────────────────────────────────────────────
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

  // ── 5. Building and shipping ──────────────────────────────────────────────────
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
