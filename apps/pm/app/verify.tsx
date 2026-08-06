import React, { useCallback, useEffect, useRef, useState } from 'react';
import { View, Text, StyleSheet, ScrollView, KeyboardAvoidingView, Platform, Pressable } from 'react-native';
import { ApiError, Button, Card, Field, apiFromEnv, color, font, space } from '@dwellm8/mobile-shared';
import { useSession } from '../src/auth/session';

/**
 * A password proves possession of a password. Before a firm is minted under an
 * email address — and every owner statement and payout notice goes to it — that
 * address has to be proved to reach the manager (#282).
 */
export default function Verify() {
  const { session, refreshVerification, signOut } = useSession();
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [note, setNote] = useState('');
  const [wait, setWait] = useState(0);
  const sent = useRef(false);

  const api = useCallback(
    () => apiFromEnv(async () => session?.idToken ?? null),
    [session],
  );

  const send = useCallback(async () => {
    setError('');
    setBusy(true);
    try {
      const client = api();
      if (!client) throw new Error('This build has no API configured');
      const out = await client.sendEmailCode();
      setWait(out.resend_after_seconds ?? 60);
      setNote(`Code sent to ${out.email ?? session?.email ?? 'your inbox'}`);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Could not send the code';
      // A refused send says when to come back — the minute between codes, or
      // the hour's ceiling — so the screen counts down instead of repeating a
      // refusal somebody can do nothing about.
      const wait = e instanceof ApiError ? e.retryAfterSeconds : undefined;
      if (wait) {
        setWait(wait);
        setNote(msg);
      } else {
        setError(msg);
      }
    } finally {
      setBusy(false);
    }
  }, [api, session]);

  // One code on arrival. The ref, not the effect deps, is what stops a second.
  useEffect(() => {
    if (sent.current || !session) return;
    sent.current = true;
    void send();
  }, [send, session]);

  useEffect(() => {
    if (wait <= 0) return;
    const t = setTimeout(() => setWait((w) => w - 1), 1000);
    return () => clearTimeout(t);
  }, [wait]);

  const confirm = async () => {
    setError('');
    setBusy(true);
    try {
      const client = api();
      if (!client) throw new Error('This build has no API configured');
      await client.confirmEmailCode(code.trim());
      await refreshVerification();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'That code did not work');
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
          <Text style={s.sub}>{session?.email}</Text>
        </View>

        <Card>
          <Text style={s.h}>Verify your email</Text>
          <Text style={s.note}>
            We sent a six-digit code. It expires in ten minutes, and your firm
            cannot be created until this address is proved.
          </Text>
          <Field
            label="Verification code"
            value={code}
            onChange={(v) => setCode(v.replace(/[^0-9]/g, '').slice(0, 6))}
            placeholder="123456"
            keyboardType="numeric"
          />
          {note && !error ? <Text style={s.ok}>{note}</Text> : null}
          {error ? <Text style={s.error} accessibilityRole="alert">{error}</Text> : null}
          <Button
            label={busy ? 'Checking…' : 'Verify'}
            onPress={confirm}
            disabled={busy || code.length !== 6}
          />
          <Pressable onPress={send} disabled={busy || wait > 0} hitSlop={10} style={{ marginTop: space(4) }}>
            <Text style={[s.switch, wait > 0 && { color: color.inkSoft }]}>
              {wait > 0 ? `Resend in ${countdown(wait)}` : 'Send another code'}
            </Text>
          </Pressable>
          <Pressable onPress={signOut} hitSlop={10} style={{ marginTop: space(3) }}>
            <Text style={s.switch}>Sign out</Text>
          </Pressable>
        </Card>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

// The hour's ceiling is half an hour of waiting at worst, and "1800s" is not
// a number anybody reads as a length of time.
const countdown = (secs: number) => (secs > 90 ? `${Math.ceil(secs / 60)} min` : `${secs}s`);

const s = StyleSheet.create({
  hero: { alignItems: 'center', marginBottom: space(8) },
  wordmark: { ...font.h1, fontSize: 32, color: color.inkStrong },
  sub: { ...font.body, color: color.inkSoft, marginTop: space(1) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(3) },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19, marginBottom: space(4) },
  ok: { ...font.small, color: color.inkSoft, marginBottom: space(3) },
  error: { ...font.small, color: color.negative, marginBottom: space(3) },
  switch: { ...font.label, color: color.accent, textAlign: 'center' },
});
