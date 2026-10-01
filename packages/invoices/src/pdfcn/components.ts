/** Adapted from pdfcn's Forme text, branded page-header, table and key-value primitives.
 * Copyright (c) 2026 Shadcn Labs. MIT; see ../../THIRD_PARTY_NOTICES.md.
 */

import type { Style } from "@formepdf/react";
import {
  Cell,
  Fixed,
  Table as FormeTable,
  Text as FormeText,
  Image,
  Row,
  View,
} from "@formepdf/react";
import type { ReactNode } from "react";
import { createElement as h } from "react";
import type { InvoicePdfLogo } from "../pdf-logo.ts";

export const theme = {
  colors: {
    foreground: "#172b2b",
    mutedForeground: "#526661",
    background: "#ffffff",
    primary: "#204e43",
    primaryForeground: "#ffffff",
    border: "#dfe8e3",
    muted: "#f3f6f3",
  },
  spacing: { sectionGap: 18, paragraphGap: 4 },
};

export function Text({ children, style = {} }: { children?: ReactNode; style?: Style }) {
  return h(
    FormeText,
    {
      style: {
        color: theme.colors.foreground,
        fontFamily: "Noto Sans",
        fontSize: 10,
        lineHeight: 1.35,
        marginBottom: theme.spacing.paragraphGap,
        ...style,
      },
    },
    children,
  );
}

export function PageHeader({
  title,
  subtitle,
  rightText,
  logo,
}: {
  title: string;
  subtitle: string;
  rightText: string;
  logo?: InvoicePdfLogo;
}) {
  const heading = [
    h(
      Text,
      {
        key: "title",
        style: {
          fontSize: 22,
          fontWeight: 700,
          color: theme.colors.primaryForeground,
          marginBottom: 8,
        },
      },
      title,
    ),
    h(
      Text,
      {
        key: "subtitle",
        style: { fontSize: 9, color: theme.colors.primaryForeground, marginBottom: 0 },
      },
      subtitle,
    ),
  ];
  const scale = logo ? Math.min(120 / logo.width, 56 / logo.height) : 1;
  return h(
    View,
    {
      wrap: false,
      style: {
        backgroundColor: theme.colors.primary,
        padding: 16,
        marginBottom: theme.spacing.sectionGap,
        borderRadius: 4,
      },
    },
    ...(logo
      ? [
          h(
            View,
            { key: "brand", style: { flexDirection: "row", gap: 16, alignItems: "flex-start" } },
            h(View, { style: { flex: 1 } }, ...heading),
            h(
              View,
              {
                style: {
                  width: 128,
                  height: 64,
                  padding: 4,
                  backgroundColor: "#ffffff",
                  borderRadius: 2,
                  alignItems: "center",
                  justifyContent: "center",
                },
              },
              h(Image, {
                src: `data:${logo.mimeType};base64,${logo.dataBase64}`,
                width: logo.width * scale,
                height: logo.height * scale,
                alt: "Company logo",
              }),
            ),
          ),
        ]
      : heading),
    h(
      Text,
      {
        style: {
          fontSize: 9,
          color: theme.colors.primaryForeground,
          marginTop: 12,
          marginBottom: 0,
        },
      },
      rightText,
    ),
  );
}

export function PageFooter({ leftText }: { leftText: string }) {
  return h(
    Fixed,
    {
      position: "footer",
      style: { borderTopWidth: 0.5, borderTopColor: theme.colors.border, paddingTop: 9 },
    },
    h(
      View,
      { style: { flexDirection: "row", justifyContent: "space-between" } },
      h(
        Text,
        { style: { fontSize: 8, color: theme.colors.mutedForeground, marginBottom: 0 } },
        leftText,
      ),
      h(
        Text,
        { style: { fontSize: 8, color: theme.colors.mutedForeground, marginBottom: 0 } },
        "Page {{pageNumber}} of {{totalPages}}",
      ),
    ),
  );
}

export function TableCell({
  children,
  align = "left",
  header = false,
}: {
  children?: string;
  align?: "left" | "center" | "right";
  header?: boolean;
}) {
  return h(
    Cell,
    { style: { paddingTop: 9, paddingBottom: 9, paddingLeft: 8, paddingRight: 8 } },
    h(
      Text,
      {
        style: {
          fontSize: 9,
          marginBottom: 0,
          textAlign: align,
          color: header ? theme.colors.primaryForeground : theme.colors.foreground,
          fontWeight: header ? 700 : 400,
        },
      },
      children,
    ),
  );
}

export function TableRow({
  children,
  header = false,
  stripe = false,
}: {
  children?: ReactNode;
  header?: boolean;
  stripe?: boolean;
}) {
  return h(
    Row,
    {
      header,
      style: {
        backgroundColor: header
          ? theme.colors.primary
          : stripe
            ? theme.colors.muted
            : theme.colors.background,
        borderBottomWidth: 0.5,
        borderBottomColor: theme.colors.border,
      },
    },
    children,
  );
}

/** Native Forme tables repeat pdfcn-styled headers and paginate whole rows. */
export function Table({ children }: { children?: ReactNode }) {
  return h(
    FormeTable,
    {
      columns: [
        { width: { fraction: 0.38 } },
        { width: { fraction: 0.08 } },
        { width: { fraction: 0.19 } },
        { width: { fraction: 0.17 } },
        { width: { fraction: 0.18 } },
      ],
    },
    children,
  );
}

export function KeyValue({
  items,
}: {
  items: { key: string; value: string; prominent?: boolean }[];
}) {
  return h(
    View,
    { wrap: false, style: { width: 245 } },
    ...items.map((item) =>
      h(
        View,
        {
          key: item.key,
          style: {
            flexDirection: "row",
            justifyContent: "space-between",
            borderBottomWidth: 0.5,
            borderBottomColor: theme.colors.border,
            paddingTop: 8,
            paddingBottom: 8,
          },
        },
        h(
          Text,
          {
            style: {
              fontSize: item.prominent ? 13 : 10,
              fontWeight: item.prominent ? 700 : 400,
              marginBottom: 0,
            },
          },
          item.key,
        ),
        h(
          Text,
          {
            style: {
              fontSize: item.prominent ? 13 : 10,
              fontWeight: item.prominent ? 700 : 400,
              marginBottom: 0,
              textAlign: "right",
            },
          },
          item.value,
        ),
      ),
    ),
  );
}
