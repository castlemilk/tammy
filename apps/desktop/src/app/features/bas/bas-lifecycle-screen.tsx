import type { TammyConveyorModule } from "../../../conveyor/client";
import { BasPreparationLifecycle } from "./bas-lifecycle";
import { BasScreen } from "./bas-screen";
import { BASLodgerRoleControl } from "./business-lodger-role-control";

type LegacyProps = Parameters<typeof BasScreen>[0];
type RoleControlApi = Pick<TammyConveyorModule<"organisation">, "getCurrentUser" | "assignRoles">;

export function BasLifecycleScreen({
  lifecycleApi,
  identityApi,
  reportApi,
  roleControlApi,
  initialPreparationTarget,
  ...legacyProps
}: Omit<LegacyProps, "roleControlApi" | "purchaseReviewOnly"> & {
  readonly lifecycleApi: TammyConveyorModule<"basPreparation">;
  readonly identityApi: Pick<
    TammyConveyorModule<"organisation">,
    "assertTotp" | "getCurrentUser" | "getOrganisation" | "updateOrganisation"
  >;
  readonly reportApi: Pick<TammyConveyorModule<"reporting">, "exportBasPreparation">;
  readonly roleControlApi: RoleControlApi;
  readonly initialPreparationTarget?: {
    readonly id: string;
    readonly revision: bigint;
    readonly hash: string;
  };
}) {
  return (
    <div className="mx-auto grid w-full max-w-[1200px] gap-6">
      <BASLodgerRoleControl api={roleControlApi} workspace={legacyProps.workspace} />
      <BasPreparationLifecycle
        api={lifecycleApi}
        identityApi={identityApi}
        reportApi={reportApi}
        {...(legacyProps.onBasDraftChanged ? { onChanged: legacyProps.onBasDraftChanged } : {})}
        {...(initialPreparationTarget ? { initialPreparationTarget } : {})}
        workspace={legacyProps.workspace}
      />
      <details className="work-surface group overflow-hidden">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-4 px-5 py-4 text-[15px] font-semibold [&::-webkit-details-marker]:hidden max-[600px]:flex-wrap">
          <span>Saved workpapers and exports</span>
          <span className="text-[12px] font-normal text-muted-foreground">
            Existing local GST workpapers, reports and purchase-source review
          </span>
          <span
            aria-hidden="true"
            className="rounded-full border border-border px-3 py-1.5 text-[12px] font-medium group-open:hidden"
          >
            Open
          </span>
        </summary>
        <div className="border-t border-border p-5">
          <BasScreen {...legacyProps} />
        </div>
      </details>
    </div>
  );
}
