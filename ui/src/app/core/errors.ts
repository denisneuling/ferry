import { ApiError } from './api';

/**
 * What to tell a person when something fails, in everyday words. The codes
 * are the API's (api/openapi/ferry-v1.yaml); the server's own wording, file
 * names and paths never reach these messages.
 */
export function describe(err: unknown): string {
  const code = err instanceof ApiError ? err.code : 'internal';
  switch (code) {
    case 'access_denied':
      return $localize`:@@err.accessDenied:The NAS does not allow you to do this here.`;
    case 'not_found':
      return $localize`:@@err.notFound:It is no longer there. Someone may have moved or deleted it.`;
    case 'exists':
      return $localize`:@@err.exists:Something with this name is already there.`;
    case 'busy':
      return $localize`:@@err.busy:The file is in use — maybe it is still being downloaded or open in another program. Try again in a moment.`;
    case 'not_empty':
      return $localize`:@@err.notEmpty:The folder is not empty.`;
    case 'not_a_folder':
      return $localize`:@@err.notAFolder:That is a file, not a folder.`;
    case 'is_a_folder':
      return $localize`:@@err.isAFolder:That is a folder, not a file.`;
    case 'invalid_path':
      return $localize`:@@err.invalidPath:This name cannot be used.`;
    case 'too_large':
      return $localize`:@@err.tooLarge:This is larger than this ferry allows.`;
    case 'server_unreachable':
      return $localize`:@@err.unreachable:The NAS does not answer right now.`;
    case 'session_lost':
      return $localize`:@@err.sessionLost:The connection to the NAS was lost. Please sign in again.`;
    case 'unauthenticated':
      return $localize`:@@err.unauthenticated:You have been signed out. Please sign in again.`;
    case 'rate_limited':
      return $localize`:@@err.rateLimited:Too many attempts. Wait a minute, then try again.`;
    case 'too_many_sessions':
      return $localize`:@@err.tooManySessions:ferry is busy right now. Try again later.`;
    case 'network':
      return $localize`:@@err.network:ferry cannot be reached. Check the connection.`;
    default:
      return $localize`:@@err.internal:Something went wrong.`;
  }
}
