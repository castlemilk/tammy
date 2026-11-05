import type { InvoiceDraftData } from "@tammy/invoices/model";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import type {
  CompanyInvoiceTemplate,
  InvoiceTemplateDefaults,
} from "../../../shared/invoice-template";
import { InvoiceLogo } from "./invoice-logo";
import { type InvoiceRecord, InvoicesScreen } from "./invoices-screen";

const defaults: InvoiceTemplateDefaults = {
  sellerAddress: "8 Collins Street, Melbourne VIC 3000",
  notes: "Thank you for your business.",
  paymentInstructions: "BSB 123-456 · Account 12345678",
  paymentTermsDays: 30,
  logoAssetId: "logo-1",
};

function fixture() {
  let template: CompanyInvoiceTemplate = { version: 1, defaults: { ...defaults } };
  let records: InvoiceRecord[] = [];
  const calls = { uploads: [] as File[], savedLogos: [] as (string | undefined)[] };
  const api = {
    list: async () => structuredClone(records),
    listExpenses: async () => [],
    getProposal: async () => undefined,
    getTemplate: async () => structuredClone(template),
    saveTemplate: async (value: InvoiceTemplateDefaults, expectedVersion: number) => {
      if (expectedVersion !== template.version) throw new Error("conflict");
      template = { version: template.version + 1, defaults: { ...value } };
      return structuredClone(template);
    },
    uploadLogo: async (file: File) => {
      calls.uploads.push(file);
      return "logo-2";
    },
    getLogo: async (id: string) => ({ id, dataUrl: "data:image/png;base64,aGVsbG8=" }),
    save: async (
      draft: InvoiceDraftData,
      id?: string,
      expectedVersion?: number,
      logoAssetId?: string,
    ) => {
      calls.savedLogos.push(logoAssetId);
      const record: InvoiceRecord = {
        id: id ?? "invoice-1",
        version: (expectedVersion ?? 0) + 1,
        state: "draft",
        sellerName: "Gamma Systems Pty Ltd",
        sellerAbn: "51824753556",
        draft: structuredClone(draft),
        ...(logoAssetId ? { logoAssetId } : {}),
      };
      records = [record];
      return structuredClone(record);
    },
    issue: async () => {
      throw new Error("unused");
    },
    exportPdf: async () => ({ saved: true }),
  };
  return {
    api,
    calls,
    setTemplate: (value: typeof template) => {
      template = value;
    },
    setRecords: (value: InvoiceRecord[]) => {
      records = value;
    },
  };
}

function savedInvoice(state: InvoiceRecord["state"] = "draft"): InvoiceRecord {
  return {
    id: "invoice-saved",
    version: 3,
    state,
    sellerName: "Gamma Systems Pty Ltd",
    sellerAbn: "51824753556",
    logoAssetId: "original-logo",
    draft: {
      number: "INV-ORIGINAL",
      issueDate: "2026-01-31",
      dueDate: "2026-02-14",
      customerName: "Original customer",
      customerAddress: "1 Customer Street",
      sellerAddress: "Original seller address",
      gstRegistered: false,
      notes: "Original notes",
      paymentInstructions: "Original bank",
      lines: [
        {
          description: "Consulting",
          quantityMilli: 1000,
          unitPriceMinor: 10000,
          taxTreatment: "out-of-scope",
        },
      ],
    },
  };
}

async function manageTemplate() {
  await userEvent.click(screen.getByRole("button", { name: "Manage invoice template" }));
  return screen.getByRole("region", { name: "Company invoice template" });
}

it("loads company defaults into an untouched new invoice using calendar days", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Seller address")).toHaveProperty("value", defaults.sellerAddress),
  );
  expect(screen.getByLabelText("Notes")).toHaveProperty("value", defaults.notes);
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty(
    "value",
    defaults.paymentInstructions,
  );
  const issueDate = (screen.getByLabelText("Issue date") as HTMLInputElement).value;
  const expected = new Date(`${issueDate}T00:00:00Z`);
  expected.setUTCDate(expected.getUTCDate() + 30);
  expect(screen.getByLabelText("Due date")).toHaveProperty(
    "value",
    expected.toISOString().slice(0, 10),
  );
  expect(await screen.findByRole("img", { name: "Invoice logo" })).toBeTruthy();
});

it("does not overwrite any draft edit when the template arrives late", async () => {
  const { api } = fixture();
  let resolveTemplate: ((value: Awaited<ReturnType<typeof api.getTemplate>>) => void) | undefined;
  render(
    <InvoicesScreen
      api={{
        ...api,
        getTemplate: () =>
          new Promise((resolve) => {
            resolveTemplate = resolve;
          }),
      }}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  fireEvent.change(screen.getByLabelText("Customer name"), {
    target: { value: "Current customer" },
  });
  fireEvent.change(screen.getByLabelText("Payment instructions"), {
    target: { value: "Current instructions" },
  });
  await act(async () => resolveTemplate?.(await api.getTemplate()));
  expect(screen.getByLabelText("Customer name")).toHaveProperty("value", "Current customer");
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty(
    "value",
    "Current instructions",
  );
  expect(screen.getByLabelText("Seller address")).toHaveProperty("value", "");
  await manageTemplate();
  await userEvent.click(screen.getByRole("button", { name: "Apply saved template" }));
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty(
    "value",
    defaults.paymentInstructions,
  );
  expect(screen.getByLabelText("Customer name")).toHaveProperty("value", "Current customer");
});

it("saves company defaults separately from current edits and uses them for subsequent invoices", async () => {
  render(<InvoicesScreen api={fixture().api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Payment instructions")).toHaveProperty(
      "value",
      defaults.paymentInstructions,
    ),
  );
  fireEvent.change(screen.getByLabelText("Payment instructions"), {
    target: { value: "Only this invoice" },
  });
  const panel = await manageTemplate();
  fireEvent.change(within(panel).getByLabelText("Default payment instructions"), {
    target: { value: "New bank details" },
  });
  fireEvent.change(within(panel).getByLabelText("Payment terms (days)"), {
    target: { value: "0" },
  });
  await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
  await screen.findByText("Company template saved.");
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty(
    "value",
    "Only this invoice",
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await userEvent.click(screen.getByRole("button", { name: "Discard changes and continue" }));
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty("value", "New bank details");
  expect(screen.getByLabelText("Due date")).toHaveProperty(
    "value",
    (screen.getByLabelText("Issue date") as HTMLInputElement).value,
  );
});

it("preserves template entries after a conflict and reloads its version for retry", async () => {
  const { api, setTemplate } = fixture();
  render(<InvoicesScreen api={api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  const panel = await manageTemplate();
  await waitFor(() =>
    expect(within(panel).getByLabelText("Default payment instructions")).toHaveProperty(
      "value",
      defaults.paymentInstructions,
    ),
  );
  fireEvent.change(within(panel).getByLabelText("Default payment instructions"), {
    target: { value: "My new instructions" },
  });
  setTemplate({ version: 2, defaults: { ...defaults, paymentInstructions: "Another editor" } });
  await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
  expect(await within(panel).findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("Your entries are still here"),
  );
  expect(within(panel).getByLabelText("Default payment instructions")).toHaveProperty(
    "value",
    "My new instructions",
  );
  await userEvent.click(
    within(panel).getByRole("button", { name: "Reload latest template version" }),
  );
  expect(within(panel).getByLabelText("Default payment instructions")).toHaveProperty(
    "value",
    "My new instructions",
  );
  await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
  await within(panel).findByText("Company template saved.");
});

it("rejects unsupported and oversized logo files before uploading", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  const panel = await manageTemplate();
  const input = within(panel).getByLabelText("Company logo");
  fireEvent.change(input, {
    target: { files: [new File(["svg"], "logo.svg", { type: "image/svg+xml" })] },
  });
  expect(await within(panel).findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("PNG or JPEG"),
  );
  fireEvent.change(input, {
    target: { files: [new File([new Uint8Array(1_048_577)], "logo.png", { type: "image/png" })] },
  });
  expect(within(panel).getByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("1 MiB"),
  );
  expect(calls.uploads).toEqual([]);
});

it("uploads a valid logo and explicitly clears its draft metadata when removed from the template", async () => {
  const { api, calls } = fixture();
  render(<InvoicesScreen api={api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  const panel = await manageTemplate();
  await within(panel).findByRole("img", { name: "Company logo preview" });
  fireEvent.change(within(panel).getByLabelText("Company logo"), {
    target: { files: [new File(["png"], "logo.png", { type: "image/png" })] },
  });
  await waitFor(() => expect(calls.uploads).toHaveLength(1));
  await waitFor(() =>
    expect(within(panel).getByRole("button", { name: "Save template" }).matches(":disabled")).toBe(
      false,
    ),
  );
  await userEvent.click(within(panel).getByRole("button", { name: "Remove logo" }));
  await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
  await within(panel).findByText("Company template saved.");
  await userEvent.click(within(panel).getByRole("button", { name: "Apply saved template" }));
  expect(screen.queryByRole("img", { name: "Invoice logo" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Description 1"), { target: { value: "Consulting" } });
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.savedLogos).toEqual([""]);
});

it("retains saved invoice branding across a restart and changing company defaults", async () => {
  const { api, calls, setTemplate } = fixture();
  const first = render(<InvoicesScreen api={api} />);
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await screen.findByRole("img", { name: "Invoice logo" });
  fireEvent.change(screen.getByLabelText("Description 1"), { target: { value: "Consulting" } });
  fireEvent.change(screen.getByLabelText("Invoice number"), { target: { value: "INV-SNAPSHOT" } });
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.savedLogos).toEqual(["logo-1"]);
  first.unmount();
  setTemplate({
    version: 2,
    defaults: { ...defaults, logoAssetId: "logo-2", paymentInstructions: "Different bank" },
  });
  render(<InvoicesScreen api={api} />);
  await waitFor(() =>
    expect(screen.getByLabelText("Payment instructions")).toHaveProperty("value", "Different bank"),
  );
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-SNAPSHOT" }));
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty(
    "value",
    defaults.paymentInstructions,
  );
  await userEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft saved.");
  expect(calls.savedLogos).toEqual(["logo-1", "logo-1"]);
});

it("keeps the original saved invoice when defaults arrive after opening it", async () => {
  const { api, setRecords } = fixture();
  setRecords([savedInvoice()]);
  let resolveTemplate: ((value: Awaited<ReturnType<typeof api.getTemplate>>) => void) | undefined;
  render(
    <InvoicesScreen
      api={{
        ...api,
        getTemplate: () =>
          new Promise((resolve) => {
            resolveTemplate = resolve;
          }),
      }}
    />,
  );
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-ORIGINAL" }));
  await act(async () => resolveTemplate?.(await api.getTemplate()));
  expect(screen.getByLabelText("Seller address")).toHaveProperty(
    "value",
    "Original seller address",
  );
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty("value", "Original bank");
  expect(screen.getByLabelText("Due date")).toHaveProperty("value", "2026-02-14");
  expect(screen.getByRole("button", { name: "Export PDF" })).toHaveProperty("disabled", false);
});

it("applies calendar terms across a month boundary without changing customer or line details", async () => {
  const { api, setRecords } = fixture();
  setRecords([savedInvoice()]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-ORIGINAL" }));
  const panel = await manageTemplate();
  await userEvent.click(within(panel).getByRole("button", { name: "Apply saved template" }));
  expect(screen.getByLabelText("Due date")).toHaveProperty("value", "2026-03-02");
  expect(screen.getByLabelText("Customer name")).toHaveProperty("value", "Original customer");
  expect(screen.getByLabelText("Description 1")).toHaveProperty("value", "Consulting");
  expect(screen.getByLabelText("Unit price 1")).toHaveProperty("value", "100.00");
  expect(screen.getByRole("button", { name: "Export PDF" })).toHaveProperty("disabled", true);
});

it("leaves issued invoices immutable while allowing company defaults to be managed", async () => {
  const { api, setRecords } = fixture();
  setRecords([savedInvoice("issued")]);
  render(<InvoicesScreen api={api} />);
  await userEvent.click(await screen.findByRole("button", { name: "Open INV-ORIGINAL" }));
  await userEvent.click(screen.getByText("Company defaults for new invoices"));
  const panel = await manageTemplate();
  expect(within(panel).getByRole("button", { name: "Apply saved template" })).toHaveProperty(
    "disabled",
    true,
  );
  expect(screen.queryByLabelText("Payment instructions")).toBeNull();
  await userEvent.click(screen.getByText("Invoice information & private estimates"));
  const issuedItems = screen.getByRole("region", { name: "Issued invoice items" });
  expect(issuedItems.querySelector("input, select, textarea")).toBeNull();
  expect(issuedItems.textContent).toContain("Original bank");
  expect(screen.queryByRole("button", { name: "Remove invoice logo" })).toBeNull();
  fireEvent.change(within(panel).getByLabelText("Default payment instructions"), {
    target: { value: "Future bank" },
  });
  await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
  await within(panel).findByText("Company template saved.");
  expect(issuedItems.textContent).toContain("Original bank");
  expect(issuedItems.textContent).not.toContain("Future bank");
});

it("retries a failed defaults load without replacing edits made before retry", async () => {
  const { api } = fixture();
  let loads = 0;
  render(
    <InvoicesScreen
      api={{
        ...api,
        getTemplate: async () => {
          if (loads++ === 0) throw new Error("unavailable");
          return await api.getTemplate();
        },
      }}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await screen.findByText(
    "Company defaults could not be loaded. Your invoice edits are unchanged.",
  );
  fireEvent.change(screen.getByLabelText("Payment instructions"), {
    target: { value: "My instructions" },
  });
  await userEvent.click(screen.getByRole("button", { name: "Retry template" }));
  const panel = await manageTemplate();
  await waitFor(() =>
    expect(within(panel).getByLabelText("Default payment instructions")).toHaveProperty(
      "value",
      defaults.paymentInstructions,
    ),
  );
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty("value", "My instructions");
  expect(screen.getByLabelText("Seller address")).toHaveProperty("value", "");
});

it("preserves template edits and original logo after a failed upload then allows retry", async () => {
  const { api } = fixture();
  let uploads = 0;
  render(
    <InvoicesScreen
      api={{
        ...api,
        uploadLogo: async (file) => {
          if (uploads++ === 0) throw new Error("malformed image");
          return await api.uploadLogo(file);
        },
      }}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  const panel = await manageTemplate();
  await within(panel).findByRole("img", { name: "Company logo preview" });
  fireEvent.change(within(panel).getByLabelText("Default notes"), {
    target: { value: "Retain these notes" },
  });
  const file = new File(["png"], "logo.png", { type: "image/png" });
  fireEvent.change(within(panel).getByLabelText("Company logo"), { target: { files: [file] } });
  expect(await within(panel).findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("could not be uploaded"),
  );
  expect(within(panel).getByLabelText("Default notes")).toHaveProperty(
    "value",
    "Retain these notes",
  );
  expect(within(panel).getByRole("img", { name: "Company logo preview" })).toBeTruthy();
  fireEvent.change(within(panel).getByLabelText("Company logo"), { target: { files: [file] } });
  await within(panel).findByText("Logo uploaded. Save the template to use it for new invoices.");
  expect(uploads).toBe(2);
  expect(within(panel).getByLabelText("Default notes")).toHaveProperty(
    "value",
    "Retain these notes",
  );
});

it.each(["", "-1", "1.5", "366"])(
  "rejects invalid payment terms %j without losing entries",
  async (value) => {
    render(<InvoicesScreen api={fixture().api} />);
    await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
    const panel = await manageTemplate();
    await waitFor(() =>
      expect(within(panel).getByLabelText("Payment terms (days)")).toHaveProperty("value", "30"),
    );
    fireEvent.change(within(panel).getByLabelText("Default notes"), {
      target: { value: "Keep my notes" },
    });
    fireEvent.change(within(panel).getByLabelText("Payment terms (days)"), { target: { value } });
    await userEvent.click(within(panel).getByRole("button", { name: "Save template" }));
    expect(await within(panel).findByRole("alert")).toHaveProperty(
      "textContent",
      "Payment terms must be a whole number from 0 to 365 days.",
    );
    expect(within(panel).getByLabelText("Default notes")).toHaveProperty("value", "Keep my notes");
  },
);

it("ignores a stale logo response after switching the selected asset", async () => {
  const pending = new Map<string, (value: { id: string; dataUrl: string }) => void>();
  const api = {
    getLogo: (id: string) =>
      new Promise<{ id: string; dataUrl: string }>((resolve) => {
        pending.set(id, resolve);
      }),
  };
  const view = render(<InvoiceLogo api={api} id="old-logo" label="Invoice logo" />);
  view.rerender(<InvoiceLogo api={api} id="new-logo" label="Invoice logo" />);
  await act(async () =>
    pending.get("new-logo")?.({ id: "new-logo", dataUrl: "data:image/png;base64,bmV3" }),
  );
  expect(screen.getByRole("img", { name: "Invoice logo" }).getAttribute("src")).toBe(
    "data:image/png;base64,bmV3",
  );
  await act(async () =>
    pending.get("old-logo")?.({ id: "old-logo", dataUrl: "data:image/png;base64,b2xk" }),
  );
  expect(screen.getByRole("img", { name: "Invoice logo" }).getAttribute("src")).toBe(
    "data:image/png;base64,bmV3",
  );
});

it("retries a failed logo preview", async () => {
  let attempts = 0;
  render(
    <InvoiceLogo
      api={{
        getLogo: async (id) => {
          if (attempts++ === 0) throw new Error("unavailable");
          return { id, dataUrl: "data:image/png;base64,bG9nbw==" };
        },
      }}
      id="logo-1"
      label="Invoice logo"
    />,
  );
  await userEvent.click(await screen.findByRole("button", { name: "Retry invoice logo" }));
  expect(await screen.findByRole("img", { name: "Invoice logo" })).toBeTruthy();
  expect(attempts).toBe(2);
});

it("applies late defaults to an untouched explicitly opened new invoice", async () => {
  const { api } = fixture();
  let resolveTemplate: ((value: Awaited<ReturnType<typeof api.getTemplate>>) => void) | undefined;
  render(
    <InvoicesScreen
      api={{
        ...api,
        getTemplate: () =>
          new Promise((resolve) => {
            resolveTemplate = resolve;
          }),
      }}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "New invoice" }));
  await act(async () => resolveTemplate?.(await api.getTemplate()));
  expect(screen.getByLabelText("Seller address")).toHaveProperty("value", defaults.sellerAddress);
  expect(screen.getByLabelText("Payment instructions")).toHaveProperty(
    "value",
    defaults.paymentInstructions,
  );
});
