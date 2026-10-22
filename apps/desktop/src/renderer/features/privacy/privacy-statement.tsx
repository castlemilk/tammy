import { readPublicLinks } from "../../../shared/public-links";
import { Button } from "../../components/ui/button";

export function PrivacyStatement() {
  const links = readPublicLinks();
  return (
    <section
      className="rounded-[6px] border border-border bg-surface p-4"
      aria-labelledby="privacy-heading"
    >
      <h2 className="text-[12px] font-semibold text-foreground" id="privacy-heading">
        Privacy
      </h2>
      <div className="mt-2 grid gap-2 text-[10px] leading-5 text-muted-foreground">
        <p>
          Tammy stores your accounting records in an encrypted workspace on this Mac. The current
          app does not send those records to Tammy or third parties and does not include analytics,
          advertising or tracking.
        </p>
        <p>
          Files are read only when you choose them for import. Workspace secrets are kept using the
          macOS Keychain. Local records remain until you remove the workspace or the app&apos;s
          data.
        </p>
        {links.privacyPolicy && links.support ? (
          <p className="flex flex-wrap gap-x-4">
            <a
              className="font-medium text-forest underline"
              href={links.privacyPolicy}
              target="_blank"
              rel="noopener"
            >
              Privacy policy
            </a>
            <a
              className="font-medium text-forest underline"
              href={links.support}
              target="_blank"
              rel="noopener"
            >
              Support
            </a>
          </p>
        ) : (
          <p>
            Public privacy and support links are required before an App Store release. This local
            statement remains available in development builds.
          </p>
        )}
      </div>
    </section>
  );
}

export function PrivacyScreen({ onBack }: { readonly onBack: () => void }) {
  return (
    <main className="grid min-h-screen place-items-center bg-background px-5 py-8">
      <div className="grid w-full max-w-[620px] gap-4">
        <div>
          <p className="font-serif text-[16px] font-bold text-forest">Tammy</p>
          <h1 className="mt-1 text-[20px] font-semibold tracking-[-0.02em] text-foreground">
            Privacy and support
          </h1>
        </div>
        <PrivacyStatement />
        <Button className="w-fit" onClick={onBack} type="button" variant="outline">
          Back to Tammy
        </Button>
      </div>
    </main>
  );
}
