import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView, KeyboardAvoidingView, Platform } from 'react-native';
import { useRouter } from 'expo-router';
import {
  Button, Card, Field, apiFromEnv, color, font, space,
} from '@dwellm8/mobile-shared';
import { useSession } from '../src/auth/session';

/**
 * The way into the manager's app. A verified sign-in is not yet a firm: a new
 * manager names theirs here, because /v1/onboarding is the only call that
 * turns a token into an organisation everything else is scoped by (#31).
 */
export default function SignIn() {
  const router = useRouter();
  const { identity, signIn, signUp } = useSession();
  const [mode, setMode] = useState<'in' | 'up'>('in');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [firm, setFirm] = useState('');
  const [naming, setNaming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const enter = async () => {
    setError('');
    setBusy(true);
    try {
      const session = mode === 'in' ? await signIn(email, password) : await signUp(email, password);
      const api = apiFromEnv(async () => session.idToken);
      if (!api) { router.replace('/(tabs)'); return; }
      if (await api.me()) { router.replace('/(tabs)'); return; }
      setNaming(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not sign in');
    } finally {
      setBusy(false);
    }
  };

  const createFirm = async () => {
    setError('');
    setBusy(true);
    try {
      const api = apiFromEnv();
      if (!api) throw new Error('This build has no API configured');
      await api.onboard(firm.trim());
      router.replace('/(tabs)');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not create the firm');
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
          <Text style={s.sub}>Property Manager</Text>
        </View>

        {!identity ? (
          <Card>
            <Text style={s.h}>Sign-in is not configured</Text>
            <Text style={s.note}>
              This build has no identity pool. Set EXPO_PUBLIC_GIP_API_KEY and
              EXPO_PUBLIC_GIP_TENANT_ID and reload.
            </Text>
          </Card>
        ) : naming ? (
          <Card>
            <Text style={s.h}>Name your firm</Text>
            <Text style={s.note}>
              Every property, owner and payout you handle sits under this firm.
            </Text>
            <Field label="Firm name" value={firm} onChange={setFirm} placeholder="Menon Properties" />
            {error ? <Text style={s.error} accessibilityRole="alert">{error}</Text> : null}
            <Button
              label={busy ? 'Creating…' : 'Create firm'}
              onPress={createFirm}
              disabled={busy || firm.trim().length < 2}
            />
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
            <Button
              label={busy ? 'Please wait…' : mode === 'in' ? 'Sign in' : 'Create account'}
              onPress={enter}
              disabled={busy || !email.trim() || password.length < 6}
            />
            <Pressable
              onPress={() => { setMode(mode === 'in' ? 'up' : 'in'); setError(''); }}
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
  switch: { ...font.label, color: color.accent, textAlign: 'center' },
});
