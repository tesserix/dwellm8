import React from 'react';
import { SignIn as SharedSignIn } from '@dwellm8/mobile-shared';
import { useSession } from '../src/auth/session';

export default function SignIn() {
  const { identity, signIn, signUp, resetPassword } = useSession();
  return (
    <SharedSignIn
      subtitle="Property Manager"
      configured={!!identity}
      actions={{ signIn, signUp, resetPassword }}
    />
  );
}
