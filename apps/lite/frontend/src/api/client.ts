// The ONLY file that imports the generated bindings (gate G1, enforced by ESLint).
//
// Every method Go binds must be referenced here (gate G2, a Go test), and every function here must be
// called from the application (gate G3, a Vitest over the TypeScript AST). A method on either side with no
// partner fails a build, which is what "zero unconnected bindings" has to mean to hold.
//
// Types are not written by hand: each result type is inferred from the file Wails generated from the Go
// struct, so a field renamed in Go is a compile error here.
import * as App from "../../wailsjs/go/api/App";
import * as Backups from "../../wailsjs/go/api/Backups";
import * as Cash from "../../wailsjs/go/api/Cash";
import * as Catalog from "../../wailsjs/go/api/Catalog";
import * as Customers from "../../wailsjs/go/api/Customers";
import * as Export from "../../wailsjs/go/api/Export";
import * as FX from "../../wailsjs/go/api/FX";
import * as Owner from "../../wailsjs/go/api/Owner";
import * as Print from "../../wailsjs/go/api/Print";
import * as Printers from "../../wailsjs/go/api/Printers";
import * as Reports from "../../wailsjs/go/api/Reports";
import * as Sales from "../../wailsjs/go/api/Sales";
import * as Settings from "../../wailsjs/go/api/Settings";
import * as Stock from "../../wailsjs/go/api/Stock";
import * as Till from "../../wailsjs/go/api/Till";
import { api } from "../../wailsjs/go/models";
import { unwrap, type Plain } from "./envelope";

export type BootStatus = Plain<api.BootStatusDTO>;
export type Health = Plain<api.HealthDTO>;
export type About = Plain<api.AboutDTO>;
export type ImportPreview = Plain<api.ImportPreviewDTO>;
export type ImportRow = Plain<api.ImportRowDTO>;
export type ImportProblem = Plain<api.ImportProblemDTO>;
export type ImportResult = Plain<api.ImportResultDTO>;
export type SettingsState = Plain<api.SettingsDTO>;
export type SettingsInput = Plain<api.SettingsInput>;
export type FirstRunInput = Plain<api.FirstRunInput>;
export type Unit = Plain<api.UnitDTO>;
export type Currency = Plain<api.CurrencyDTO>;
export type Product = Plain<api.ProductDTO>;
export type ProductQuery = Plain<api.ProductQueryDTO>;
export type CreateProductInput = Plain<api.CreateProductInput>;
export type UpdateProductInput = Plain<api.UpdateProductInput>;
export type SetPriceInput = Plain<api.SetPriceInput>;
export type SetActiveInput = Plain<api.SetActiveInput>;
export type SetQuickSlotInput = Plain<api.SetQuickSlotInput>;
export type OwnerStatus = Plain<api.OwnerStatusDTO>;
export type OwnerEvent = Plain<api.OwnerEventDTO>;
export type ChangePINInput = Plain<api.ChangePINInput>;
export type RecoverInput = Plain<api.RecoverInput>;
export type SetPackageInput = Plain<api.SetPackageInput>;
export type StockLevel = Plain<api.StockLevelDTO>;
export type Valuation = Plain<api.ValuationDTO>;
export type ValuationLine = Plain<api.ValuationLineDTO>;
export type Movement = Plain<api.MovementDTO>;
export type History = Plain<api.HistoryDTO>;
export type ReceiveInput = Plain<api.ReceiveInput>;
export type CountInput = Plain<api.CountInput>;
export type AdjustInput = Plain<api.AdjustInput>;
export type OpenPackageInput = Plain<api.OpenPackageInput>;
export type ReverseReceiptInput = Plain<api.ReverseReceiptInput>;
export type CorrectCostInput = Plain<api.CorrectCostInput>;
export type Finding = Plain<api.FindingDTO>;
export type RateState = Plain<api.RateDTO>;
export type RateFetch = Plain<api.FetchDTO>;
export type RateHistoryRow = Plain<api.RateHistoryDTO>;
export type SetRateInput = Plain<api.SetRateInput>;
export type Quote = Plain<api.QuoteDTO>;
export type CartLineInput = Plain<api.CartLineInput>;
export type CartInput = Plain<api.CartInput>;
export type CheckoutInput = Plain<api.CheckoutInput>;
export type CartLine = Plain<api.CartLineDTO>;
export type CartQuote = Plain<api.CartQuoteDTO>;
export type Scan = Plain<api.ScanDTO>;
export type CashNote = Plain<api.CashNoteDTO>;
export type Sale = Plain<api.SaleDTO>;
export type SaleLine = Plain<api.SaleLineDTO>;
export type Day = Plain<api.DayDTO>;
export type DayTotals = Plain<api.DayTotalsDTO>;
export type VoidInput = Plain<api.VoidInput>;
export type SaleFinding = Plain<api.SaleFindingDTO>;
export type Customer = Plain<api.CustomerDTO>;
export type Balance = Plain<api.BalanceDTO>;
export type CustomerQuery = Plain<api.CustomerQueryDTO>;
export type CustomerInput = Plain<api.CustomerInput>;
export type UpdateCustomerInput = Plain<api.UpdateCustomerInput>;
export type SetCustomerActiveInput = Plain<api.SetCustomerActiveInput>;
export type Statement = Plain<api.StatementDTO>;
export type Entry = Plain<api.EntryDTO>;
export type Outstanding = Plain<api.OutstandingDTO>;
export type DebtDay = Plain<api.DebtDayDTO>;
export type PaymentInput = Plain<api.PaymentInput>;
export type PaymentQuote = Plain<api.PaymentQuoteDTO>;
export type DebtAmountInput = Plain<api.DebtAmountInput>;
export type RefundInput = Plain<api.RefundInput>;
export type ReverseEntryInput = Plain<api.ReverseEntryInput>;
export type Amount = Plain<api.AmountDTO>;
export type Profit = Plain<api.ProfitDTO>;
export type Takings = Plain<api.TakingsDTO>;
export type DayReport = Plain<api.DayReportDTO>;
export type MonthReport = Plain<api.MonthReportDTO>;
export type ProductRow = Plain<api.ProductRowDTO>;
export type ProductsReport = Plain<api.ProductsReportDTO>;
export type StockReport = Plain<api.StockReportDTO>;
export type Drawer = Plain<api.DrawerDTO>;
export type DrawerCurrency = Plain<api.DrawerCurrencyDTO>;
export type CashEntry = Plain<api.CashEntryDTO>;
export type CashRecordInput = Plain<api.CashRecordInput>;
export type CashCountInput = Plain<api.CashCountInput>;
export type ExportResult = Plain<api.ExportResultDTO>;
export type ExportReportInput = Plain<api.ExportReportInput>;
export type ExportFormat = "xlsx" | "pdf";
export type PrintResult = Plain<api.PrintResultDTO>;
export type Preview = Plain<api.PreviewDTO>;
export type PrinterInfo = Plain<api.PrinterDTO>;
export type PrinterSettings = Plain<api.PrinterSettingsDTO>;
export type PrinterSettingsInput = Plain<api.PrinterSettingsInput>;
export type BackupInfo = Plain<api.BackupDTO>;
export type BackupStatus = Plain<api.BackupStatusDTO>;
export type Loss = Plain<api.LossDTO>;
export type RestoreResult = Plain<api.RestoreDTO>;

export function createClient() {
  return {
    app: {
      bootStatus: () => unwrap(App.BootStatus),
      health: () => unwrap(App.Health),
      firstRunStatus: () => unwrap(App.FirstRunStatus),
      completeFirstRun: (input: FirstRunInput) =>
        unwrap(() => App.CompleteFirstRun(api.FirstRunInput.createFrom(input))),
      about: () => unwrap(App.About),
      saveGuide: (name: string) => unwrap(() => App.SaveGuide(name)),
      saveSupportFile: (includeDatabase: boolean) => unwrap(() => App.SaveSupportFile(includeDatabase)),
    },
    settings: {
      get: () => unwrap(Settings.Get),
      update: (input: SettingsInput) => unwrap(() => Settings.Update(api.SettingsInput.createFrom(input))),
    },
    catalog: {
      units: () => unwrap(Catalog.Units),
      currencies: () => unwrap(Catalog.Currencies),
      products: (query: ProductQuery) => unwrap(() => Catalog.Products(api.ProductQueryDTO.createFrom(query))),
      product: (id: string) => unwrap(() => Catalog.Product(id)),
      importTemplate: () => unwrap(Catalog.ImportTemplate),
      importPreview: () => unwrap(Catalog.ImportPreview),
      importApply: (path: string, digest: string) => unwrap(() => Catalog.ImportApply(api.ImportApplyInput.createFrom({ path, digest }))),
      createProduct: (input: CreateProductInput) =>
        unwrap(() => Catalog.CreateProduct(api.CreateProductInput.createFrom(input))),
      updateProduct: (input: UpdateProductInput) =>
        unwrap(() => Catalog.UpdateProduct(api.UpdateProductInput.createFrom(input))),
      setPrice: (input: SetPriceInput) => unwrap(() => Catalog.SetPrice(api.SetPriceInput.createFrom(input))),
      setActive: (input: SetActiveInput) => unwrap(() => Catalog.SetActive(api.SetActiveInput.createFrom(input))),
      setQuickSlot: (input: SetQuickSlotInput) =>
        unwrap(() => Catalog.SetQuickSlot(api.SetQuickSlotInput.createFrom(input))),
      setPackage: (input: SetPackageInput) => unwrap(() => Catalog.SetPackage(api.SetPackageInput.createFrom(input))),
      clearPackage: (productId: string) => unwrap(() => Catalog.ClearPackage(productId)),
    },
    stock: {
      levels: () => unwrap(Stock.Levels),
      valuation: () => unwrap(Stock.Valuation),
      movements: (productId: string, limit: number) =>
        unwrap(() => Stock.Movements(api.MovementsQueryDTO.createFrom({ productId, limit }))),
      receive: (input: ReceiveInput) => unwrap(() => Stock.Receive(api.ReceiveInput.createFrom(input))),
      opening: (input: ReceiveInput) => unwrap(() => Stock.Opening(api.ReceiveInput.createFrom(input))),
      count: (input: CountInput) => unwrap(() => Stock.Count(api.CountInput.createFrom(input))),
      adjust: (input: AdjustInput) => unwrap(() => Stock.Adjust(api.AdjustInput.createFrom(input))),
      openPackage: (input: OpenPackageInput) => unwrap(() => Stock.OpenPackage(api.OpenPackageInput.createFrom(input))),
      reverseReceipt: (input: ReverseReceiptInput) =>
        unwrap(() => Stock.ReverseReceipt(api.ReverseReceiptInput.createFrom(input))),
      correctCost: (input: CorrectCostInput) => unwrap(() => Stock.CorrectCost(api.CorrectCostInput.createFrom(input))),
      verify: () => unwrap(Stock.Verify),
    },
    fx: {
      current: () => unwrap(FX.Current),
      history: (limit: number) => unwrap(() => FX.History(limit)),
      setRate: (input: SetRateInput) => unwrap(() => FX.SetRate(api.SetRateInput.createFrom(input))),
      refresh: () => unwrap(FX.Refresh),
      acceptProposal: (fetchId: string) => unwrap(() => FX.AcceptProposal(fetchId)),
      setMode: (mode: "automatic" | "manual") => unwrap(() => FX.SetMode(mode)),
      fetchQuote: () => unwrap(FX.FetchQuote),
    },
    till: {
      scan: (code: string) => unwrap(() => Till.Scan(code)),
      quote: (input: CartInput) => unwrap(() => Till.Quote(api.CartInput.createFrom(input))),
      checkout: (input: CheckoutInput) => unwrap(() => Till.Checkout(api.CheckoutInput.createFrom(input))),
      cashNote: () => unwrap(Till.CashNote),
      setCashNote: (note: string) => unwrap(() => Till.SetCashNote(note)),
    },
    sales: {
      list: (businessDate: string) => unwrap(() => Sales.List(businessDate)),
      receipt: (saleId: string) => unwrap(() => Sales.Receipt(saleId)),
      void: (input: VoidInput) => unwrap(() => Sales.Void(api.VoidInput.createFrom(input))),
      verify: () => unwrap(Sales.Verify),
    },
    customers: {
      search: (query: CustomerQuery) => unwrap(() => Customers.Search(api.CustomerQueryDTO.createFrom(query))),
      create: (input: CustomerInput) => unwrap(() => Customers.Create(api.CustomerInput.createFrom(input))),
      update: (input: UpdateCustomerInput) => unwrap(() => Customers.Update(api.UpdateCustomerInput.createFrom(input))),
      setActive: (input: SetCustomerActiveInput) => unwrap(() => Customers.SetActive(api.SetCustomerActiveInput.createFrom(input))),
      statement: (customerId: string, currency: string) =>
        unwrap(() => Customers.Statement(api.StatementQueryDTO.createFrom({ customerId, currency }))),
      outstanding: () => unwrap(Customers.Outstanding),
      quotePayment: (input: PaymentInput) => unwrap(() => Customers.QuotePayment(api.PaymentInput.createFrom(input))),
      recordPayment: (input: PaymentInput) => unwrap(() => Customers.RecordPayment(api.PaymentInput.createFrom(input))),
      opening: (input: DebtAmountInput) => unwrap(() => Customers.Opening(api.DebtAmountInput.createFrom(input))),
      writeOff: (input: DebtAmountInput) => unwrap(() => Customers.WriteOff(api.DebtAmountInput.createFrom(input))),
      refund: (input: RefundInput) => unwrap(() => Customers.Refund(api.RefundInput.createFrom(input))),
      reverse: (input: ReverseEntryInput) => unwrap(() => Customers.Reverse(api.ReverseEntryInput.createFrom(input))),
    },
    reports: {
      day: (date: string) => unwrap(() => Reports.Day(date)),
      month: (month: string) => unwrap(() => Reports.Month(month)),
      products: (from: string, to: string) => unwrap(() => Reports.Products(api.RangeInput.createFrom({ from, to }))),
      stock: (from: string, to: string) => unwrap(() => Reports.Stock(api.RangeInput.createFrom({ from, to }))),
    },
    cash: {
      drawer: (date: string) => unwrap(() => Cash.Drawer(date)),
      record: (input: CashRecordInput) => unwrap(() => Cash.Record(api.CashRecordInput.createFrom(input))),
      count: (input: CashCountInput) => unwrap(() => Cash.Count(api.CashCountInput.createFrom(input))),
      reverse: (entryId: string, reason: string) => unwrap(() => Cash.Reverse(api.ReverseCashInput.createFrom({ entryId, reason }))),
    },
    exports: {
      report: (input: ExportReportInput) => unwrap(() => Export.Report(api.ExportReportInput.createFrom(input))),
      statement: (customerId: string, format: ExportFormat) =>
        unwrap(() => Export.Statement(api.ExportStatementInput.createFrom({ customerId, format }))),
      debtLedger: (from: string, to: string, format: ExportFormat) =>
        unwrap(() => Export.DebtLedger(api.ExportRangeInput.createFrom({ from, to, format }))),
      salesHistory: (from: string, to: string, format: ExportFormat) =>
        unwrap(() => Export.SalesHistory(api.ExportRangeInput.createFrom({ from, to, format }))),
      showInFolder: (path: string) => unwrap(() => Export.ShowInFolder(path)),
    },
    print: {
      sale: (saleId: string) => unwrap(() => Print.Sale(saleId)),
      entry: (entryId: string) => unwrap(() => Print.Entry(entryId)),
      preview: (kind: "sale" | "entry" | "test", id: string) => unwrap(() => Print.Preview(api.PreviewInput.createFrom({ kind, id }))),
    },
    printers: {
      list: () => unwrap(Printers.List),
      settings: () => unwrap(Printers.Settings),
      save: (input: PrinterSettingsInput) => unwrap(() => Printers.Save(api.PrinterSettingsInput.createFrom(input))),
      test: () => unwrap(Printers.Test),
    },
    backups: {
      list: () => unwrap(Backups.List),
      takeNow: () => unwrap(Backups.TakeNow),
      status: () => unwrap(Backups.Status),
      lossPreview: (name: string) => unwrap(() => Backups.LossPreview(name)),
      restore: (name: string) => unwrap(() => Backups.Restore(name)),
      restoreFromFile: () => unwrap(Backups.RestoreFromFile),
      saveCopy: (name: string) => unwrap(() => Backups.SaveCopy(name)),
      setOutsideFolder: (clear: boolean) => unwrap(() => Backups.SetOutsideFolder(clear)),
    },
    owner: {
      status: () => unwrap(Owner.Status),
      elevate: (pin: string) => unwrap(() => Owner.Elevate(api.PINInput.createFrom({ pin }))),
      endElevation: () => unwrap(Owner.EndElevation),
      changePin: (input: ChangePINInput) => unwrap(() => Owner.ChangePIN(api.ChangePINInput.createFrom(input))),
      recover: (input: RecoverInput) => unwrap(() => Owner.Recover(api.RecoverInput.createFrom(input))),
      events: (limit: number) => unwrap(() => Owner.Events(limit)),
    },
  };
}

export type Client = ReturnType<typeof createClient>;
