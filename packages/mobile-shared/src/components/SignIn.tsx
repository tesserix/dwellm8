import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView, KeyboardAvoidingView, Platform } from 'react-native';
import { Button, Field } from './controls';
import { Card } from './ui';
import { color, font, space } from '../theme/tokens';

/**
 * The way into any Dwellm8 app. It only proves who signed in — what they may
 * then do is the surface's own question, so nothing here navigates.
 */

/** What the screen needs of a session, and nothing more. */
export type SignInActions = {
  signIn(email: string, password: string): Promise<unknown>;
  signUp(email: string, password: string): Promise<unknown>;
  resetPassword(email: string): Promise<void>;
};

// GIP's own floor, and the reason the button stays disabled below it.
const minPassword = 6;

export function SignIn(
  { subtitle, configured, actions }: { subtitle: string; configured: boolean; actions: SignInActions },
) {
  const [mode, setMode] = useState<'in' | 'up'>('in');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [sent, setSent] = useState('');

  const enter = async () => {
    setError('');
    setSent('');
    setBusy(true);
    try {
      if (mode === 'in') await actions.signIn(email.trim(), password);
      else await actions.signUp(email.trim(), password);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not sign in');
    } finally {
      setBusy(false);
    }
  };

  const forgot = async () => {
    setError('');
    setSent('');
    if (!email.trim()) {
      setError('Enter your email address and we will send a reset link');
      return;
    }
    setBusy(true);
    try {
      await actions.resetPassword(email.trim());
      // Never whether the address is known: that answer is an enumeration oracle.
      setSent('If that address has an account, a reset link is on its way');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not send a reset link');
    } finally {
      setBusy(false);
    }
  };

  return (
    <KeyboardAvoidingView
      style={{ flex: 1, backgroundColor: color.bgTop }}
      behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
    >
      <ScrollView contentContainerStyle={{ paddingTop: space(16), paddingBottom: space(10) }}>
        <View style={s.hero}>
          <Text style={s.wordmark}>Dwellm8</Text>
          <Text style={s.sub}>{subtitle}</Text>
        </View>

        {!configured ? (
          <Card>
            <Text style={s.h}>Sign-in is not configured</Text>
            <Text style={s.note}>
              This build has no identity pool. Set EXPO_PUBLIC_GIP_API_KEY and
              EXPO_PUBLIC_GIP_TENANT_ID and reload.
            </Text>
          </Card>
        ) : (
          <Card>
            <Text style={s.h}>{mode === 'in' ? 'Sign in' : 'Create an account'}</Text>
            <Field
              label="Email"
              value={email}
              onChange={setEmail}
              placeholder="you@firm.in"
              keyboardType="email-address"
            />
            <Field label="Password" value={password} onChange={setPassword} secure />
            {error ? <Text style={s.error} accessibilityRole="alert">{error}</Text> : null}
            {sent ? <Text style={s.sent} accessibilityRole="alert">{sent}</Text> : null}
            <Button
              label={busy ? 'Please wait…' : mode === 'in' ? 'Sign in' : 'Create account'}
              onPress={enter}
              disabled={busy || !email.trim() || password.length < minPassword}
            />
            {mode === 'in' ? (
              <Pressable onPress={forgot} disabled={busy} hitSlop={10} style={{ marginTop: space(4) }}>
                <Text style={s.switch}>Forgot your password?</Text>
              </Pressable>
            ) : null}
            <Pressable
              onPress={() => { setMode(mode === 'in' ? 'up' : 'in'); setError(''); setSent(''); }}
              hitSlop={10}
              style={{ marginTop: space(4) }}
            >
              <Text style={s.switch}>
                {mode === 'in' ? 'New here? Create an account' : 'Already have an account? Sign in'}
              </Text>
            </Pressable>
          </Card>
        )}
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({
  hero: { alignItems: 'center', marginBottom: space(8) },
  wordmark: { ...font.h1, fontSize: 32, color: color.inkStrong },
  sub: { ...font.body, color: color.inkSoft, marginTop: space(1) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(3) },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19, marginBottom: space(4) },
  error: { ...font.small, color: color.negative, marginBottom: space(3) },
  sent: { ...font.small, color: color.inkSoft, marginBottom: space(3) },
  switch: { ...font.label, color: color.accent, textAlign: 'center' },
});
