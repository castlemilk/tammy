/** Keep the existing Sites host canonical until tammy.tax serves the same deployment.
 * When the custom domain is verified: update this value, regenerate public
 * sitemap.xml / robots.txt / llms.txt, and inspect the live canonical tags.
 */
export const SITE_ORIGIN = "https://tammy-accounting.castlemilk.chatgpt.site";
