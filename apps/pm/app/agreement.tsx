import React, { useEffect, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator, Linking } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, Field, Screen, SectionTitle,
  apiFromEnv, color, font, space,
} from '@dwellm8/mobile-shared';
import { agreementQuestions, printAgreement, type AgreementQuestion, type PrintedAgreement } from '../src/data/agreement';

/**
 * The owner–manager agreement (#340). No online signing yet: this prints, both
 * sides sign paper, and the executed copy is uploaded back against the
 * property as a management_agreement (#339).
 */
export default function AgreementScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const [questions, setQuestions] = useState<AgreementQuestion[]>([]);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [printing, setPrinting] = useState(false);
  const [printed, setPrinted] = useState<PrintedAgreement | null>(null);

  useEffect(() => {
    let live = true;
    const api = apiFromEnv();
    if (!api) {
      setError('this build has no API to talk to');
      setLoading(false);
      return;
    }
    agreementQuestions(api)
      .then((qs) => { if (live) setQuestions(qs); })
      .catch((err: Error) => { if (live) setError(err.message); })
      .finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, []);

  async function print() {
    const api = apiFromEnv();
    if (!api || !id) return;
    setPrinting(true);
    setError('');
    try {
      // Every question, not only the ones typed into: an untouched field is
      // the blank one that would otherwise print as a placeholder.
      const filled = Object.fromEntries(questions.map((q) => [q.field, answers[q.field] ?? '']));
      setPrinted(await printAgreement(api, id, filled));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPrinting(false);
    }
  }

  return (
    <>
      <BackHeader
        title="Management agreement"
        subtitle="Print it, sign on paper, file the signed copy"
        onBack={() => router.back()}
      />
      <Screen>
        <Card>
          <Text style={s.h}>What this agreement binds</Text>
          <Text style={s.note}>
            You may enter the property, let it, collect rent and instruct repairs on the owner&apos;s
            behalf. You have no authority to sell, mortgage, gift or otherwise transact in it.
          </Text>
          <Text style={s.note}>
            An owner intending to sell must give you four months&apos; written notice, so the
            tenancies you placed can be managed to their end.
          </Text>
          <Text style={s.note}>
            The owner warrants the property is fit for occupation and permits the inspections in
            the onboarding checklist — termite, electrical, water and fire — before a tenancy begins.
          </Text>
        </Card>

        {loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}

        {!loading && questions.length ? (
          <>
            <SectionTitle>What the agreement needs</SectionTitle>
            <Card>
              {questions.map((q) => (
                <Field
                  key={q.field}
                  label={q.label}
                  placeholder={q.hint}
                  keyboardType={q.keyboard}
                  value={answers[q.field] ?? ''}
                  onChange={(v) => setAnswers((a) => ({ ...a, [q.field]: v }))}
                />
              ))}
              <Text style={s.note}>The property&apos;s address is filled from your register.</Text>
            </Card>
          </>
        ) : null}

        {error ? <Card><Text style={s.error}>{error}</Text></Card> : null}

        {printed ? (
          <Card>
            <Text style={s.h}>Printed</Text>
            <Text style={s.note}>{printed.filename}</Text>
            {printed.downloadUrl ? (
              <Button
                label="Open it"
                onPress={() => Linking.openURL(printed.downloadUrl as string)}
                style={{ marginTop: space(3) }}
              />
            ) : null}
            <Text style={s.note}>
              Both parties and two witnesses sign the printed copy. Upload the signed scan under
              Ownership so it sits beside the deed.
            </Text>
          </Card>
        ) : null}

        {!loading ? (
          <Button
            label={printing ? 'Printing…' : 'Print the agreement'}
            onPress={print}
            disabled={printing}
            style={{ marginTop: space(2) }}
          />
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19, marginTop: space(2) },
  error: { ...font.small, color: color.warnInk, lineHeight: 19 },
  waiting: { paddingVertical: space(6), alignItems: 'center' },
});
