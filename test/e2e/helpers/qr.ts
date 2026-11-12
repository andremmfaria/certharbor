import jsQRModule from "jsqr";
import { PNG } from "pngjs";

// jsqr's own dist/index.d.ts (export default) disagrees with its actual CJS
// shape (module.exports is the function itself, with .default pointing back
// at itself for esModule interop) in a way TS's NodeNext resolution can't
// reconcile: the default import above type-checks as the whole module
// namespace, not a callable. Both `jsQRModule` and `jsQRModule.default` are
// the same function at runtime (verified empirically), so re-typing the
// import as the callable it actually is avoids fighting the package's types.
const jsQR = jsQRModule as unknown as (
  data: Uint8ClampedArray,
  width: number,
  height: number,
) => { data: string } | null;

/**
 * E2E-AUTH-04 asserts that GET /profile/2fa/qr decodes to the same secret the
 * page renders in its readonly input. That is a property of the rendered PNG,
 * so the image is decoded rather than trusted.
 */
export function decodeQR(png: Buffer): string {
  const image = PNG.sync.read(png);
  const result = jsQR(new Uint8ClampedArray(image.data), image.width, image.height);
  if (!result) {
    throw new Error(`could not decode a QR code from a ${image.width}x${image.height} PNG (${png.length} bytes)`);
  }
  return result.data;
}

/** Pulls the shared secret out of an otpauth:// URI. */
export function secretFromOtpauth(uri: string): string {
  const secret = new URL(uri).searchParams.get("secret");
  if (!secret) throw new Error(`no secret parameter in otpauth URI: ${uri}`);
  return secret;
}
