import { writeFile } from "node:fs/promises";
import path from "node:path";
import type { Page, TestInfo } from "@playwright/test";
import { pngLogo } from "../../../../../packages/invoices/test-support/logo-fixtures.ts";
import { expect } from "../fixtures";

export const ORIGINAL_COMPANY_TEMPLATE = {
  sellerAddress: "8 Collins Street, Melbourne VIC 3000",
  notes: "Thank you for your business.",
  paymentInstructions: "Bank transfer BSB 123-456 Account 12345678",
  paymentTermsDays: 30,
} as const;

export const UPDATED_COMPANY_TEMPLATE = {
  sellerAddress: "20 Collins Street, Melbourne VIC 3000",
  notes: "Updated company payment details.",
  paymentInstructions: "Bank transfer BSB 654-321 Account 87654321",
  paymentTermsDays: 7,
} as const;

const logo = pngLogo();
export const COMPANY_LOGO_DATA_URL = `data:${logo.mimeType};base64,${logo.dataBase64}`;

type TemplateValues = {
  readonly sellerAddress: string;
  readonly notes: string;
  readonly paymentInstructions: string;
  readonly paymentTermsDays: number;
};

export async function configureCompanyInvoiceTemplate(
  page: Page,
  directory: string,
  testInfo: TestInfo,
) {
  const template = page.getByRole("region", { name: "Company invoice template", exact: true });
  await template.getByRole("button", { name: "Manage invoice template", exact: true }).click();
  await fillTemplate(page, ORIGINAL_COMPANY_TEMPLATE);
  const invalidLogoPath = path.join(directory, "invalid-company-logo.png");
  const logoPath = path.join(directory, "company-logo.png");
  await writeFile(invalidLogoPath, "This is not a PNG image.");
  await writeFile(logoPath, Buffer.from(logo.dataBase64, "base64"));
  await template.getByLabel("Company logo", { exact: true }).setInputFiles(invalidLogoPath);
  await expect(template.getByRole("alert")).toHaveText(
    "The logo could not be uploaded. Choose a valid PNG or JPEG and try again. Your template entries are still here.",
  );
  await expectCompanyTemplate(page, ORIGINAL_COMPANY_TEMPLATE, false);
  await template.getByLabel("Company logo", { exact: true }).setInputFiles(logoPath);
  await expect(template.getByRole("status")).toHaveText(
    "Logo uploaded. Save the template to use it for new invoices.",
  );
  await expectCompanyTemplate(page, ORIGINAL_COMPANY_TEMPLATE, true);
  await template.getByRole("button", { name: "Save template", exact: true }).click();
  await expect(template.getByRole("status")).toHaveText("Company template saved.");
  await expect(page.getByLabel("Seller address", { exact: true })).toHaveValue(
    "27 Cambridge Road, Mooroolbark VIC 3138",
  );
  await expect(page.getByLabel("Notes", { exact: true })).toHaveValue("Invoice acceptance");
  await expect(page.getByLabel("Payment instructions", { exact: true })).toHaveValue(
    "Bank transfer",
  );
  await expect(page.getByLabel("Due date", { exact: true })).toHaveValue("2026-10-14");
  await expect(page.getByRole("img", { name: "Invoice logo", exact: true })).toHaveCount(0);
  await captureCompanyTemplate(page, directory, "invoice-template-desktop", testInfo);
  await template.getByRole("button", { name: "Apply saved template", exact: true }).click();
  await expect(
    page.getByText("Saved company template applied. Review this draft before saving.", {
      exact: true,
    }),
  ).toBeVisible();
  await expectInvoiceTemplateSnapshot(page, ORIGINAL_COMPANY_TEMPLATE, "2026-10-30", true);
  await expect(page.getByLabel("Issue date", { exact: true })).toHaveValue("2026-09-30");
  await expect(page.getByLabel("Invoice number", { exact: true })).toHaveValue("INV-MCP-001");
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(
    "Invoice acceptance customer",
  );
  await template.getByRole("button", { name: "Close template editor", exact: true }).click();
}

export async function updateCompanyInvoiceTemplate(page: Page) {
  const template = page.getByRole("region", { name: "Company invoice template", exact: true });
  await fillTemplate(page, UPDATED_COMPANY_TEMPLATE);
  await template.getByRole("button", { name: "Remove logo", exact: true }).click();
  await expectCompanyTemplate(page, UPDATED_COMPANY_TEMPLATE, false);
  await template.getByRole("button", { name: "Save template", exact: true }).click();
  await expect(template.getByRole("status")).toHaveText("Company template saved.");
  await expectInvoiceTemplateSnapshot(page, ORIGINAL_COMPANY_TEMPLATE, "2026-10-30", true);
}

export async function expectCompanyTemplate(page: Page, values: TemplateValues, hasLogo: boolean) {
  const template = page.getByRole("region", { name: "Company invoice template", exact: true });
  await expect(template.getByLabel("Template seller address", { exact: true })).toHaveValue(
    values.sellerAddress,
  );
  await expect(template.getByLabel("Default notes", { exact: true })).toHaveValue(values.notes);
  await expect(template.getByLabel("Default payment instructions", { exact: true })).toHaveValue(
    values.paymentInstructions,
  );
  await expect(template.getByLabel("Payment terms (days)", { exact: true })).toHaveValue(
    String(values.paymentTermsDays),
  );
  if (hasLogo) {
    await expectLogo(page, "Company logo preview");
  } else {
    await expect(
      template.getByRole("img", { name: "Company logo preview", exact: true }),
    ).toHaveCount(0);
    await expect(template.getByText("No company logo selected.", { exact: true })).toBeVisible();
  }
}

export async function expectInvoiceTemplateSnapshot(
  page: Page,
  values: TemplateValues,
  dueDate: string,
  hasLogo: boolean,
) {
  await expect(page.getByLabel("Seller address", { exact: true })).toHaveValue(
    values.sellerAddress,
  );
  await expect(page.getByLabel("Notes", { exact: true })).toHaveValue(values.notes);
  await expect(page.getByLabel("Payment instructions", { exact: true })).toHaveValue(
    values.paymentInstructions,
  );
  await expect(page.getByLabel("Due date", { exact: true })).toHaveValue(dueDate);
  if (hasLogo) await expectLogo(page, "Invoice logo");
  else await expect(page.getByRole("img", { name: "Invoice logo", exact: true })).toHaveCount(0);
}

export async function captureCompanyTemplate(
  page: Page,
  directory: string,
  name: string,
  testInfo: TestInfo,
) {
  const screenshotPath = path.join(directory, `${name}.png`);
  await page.getByRole("region", { name: "Company invoice template", exact: true }).screenshot({
    animations: "disabled",
    path: screenshotPath,
  });
  await testInfo.attach(name, { contentType: "image/png", path: screenshotPath });
}

async function fillTemplate(page: Page, values: TemplateValues) {
  const template = page.getByRole("region", { name: "Company invoice template", exact: true });
  await template.getByLabel("Template seller address", { exact: true }).fill(values.sellerAddress);
  await template.getByLabel("Default notes", { exact: true }).fill(values.notes);
  await template
    .getByLabel("Default payment instructions", { exact: true })
    .fill(values.paymentInstructions);
  await template
    .getByLabel("Payment terms (days)", { exact: true })
    .fill(String(values.paymentTermsDays));
}

async function expectLogo(page: Page, label: "Company logo preview" | "Invoice logo") {
  const image = page.getByRole("img", { name: label, exact: true });
  await expect(image).toBeVisible();
  await expect(image).toHaveAttribute("src", COMPANY_LOGO_DATA_URL);
  await expect
    .poll(() =>
      image.evaluate((element: HTMLImageElement) => ({
        complete: element.complete,
        width: element.naturalWidth,
        height: element.naturalHeight,
      })),
    )
    .toEqual({ complete: true, width: logo.width, height: logo.height });
}
