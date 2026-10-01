export const draftFixture = () => ({
  number: "INV-2026-001",
  issueDate: "2026-09-30",
  dueDate: "2026-10-30",
  customerName: "Example Customer Pty Ltd",
  customerAddress: "10 Example Road\nMelbourne VIC 3000",
  customerAbn: "51824753556",
  sellerAddress: "27 Cambridge Road\nMooroolbark VIC 3138",
  gstRegistered: true,
  notes: "Thank you for your business.",
  paymentInstructions: "Bank transfer with invoice number as reference.",
  lines: [
    {
      description: "Accounting services",
      quantityMilli: 1500,
      unitPriceMinor: 10001,
      unitCostMinor: 4000,
      taxTreatment: "taxable" as const,
    },
  ],
});
