import gleam/dict
import gleam/erlang/process
import gleam/list
import gleam/option.{None, Some}
import nocontract_runtime/sign_in_check

type SignInRefusal {
  CredentialMissing(header: String)
  CredentialRejected(scope: String)
}

type SignInAudit {
  MissingCredential
  CheckedCredential
}

pub fn sign_in_uses_the_chosen_header_and_application_verifier_test() {
  list.each(
    [
      #(
        dict.from_list([#("x-shopper", "shopper-7")]),
        Some(CredentialMissing(header: "x-session")),
        MissingCredential,
      ),
      #(
        dict.from_list([#("x-session", "")]),
        Some(CredentialMissing(header: "x-session")),
        MissingCredential,
      ),
      #(dict.from_list([#("x-session", "shopper-7")]), None, CheckedCredential),
      #(
        dict.from_list([#("x-session", "shopper-8")]),
        Some(CredentialRejected(scope: "read-balance")),
        CheckedCredential,
      ),
      #(
        dict.from_list([#("x-session", " shopper-7 ")]),
        Some(CredentialRejected(scope: "read-balance")),
        CheckedCredential,
      ),
    ],
    fn(each) {
      let #(headers, expected, audit_event) = each
      let audit = process.new_subject()
      let admitted_shoppers = dict.from_list([#("shopper-7", None)])
      let missing = fn() {
        process.send(audit, MissingCredential)
        CredentialMissing(header: "x-session")
      }
      let verify = fn(credential) {
        process.send(audit, CheckedCredential)
        case dict.get(admitted_shoppers, credential) {
          Ok(decision) -> decision
          Error(Nil) -> Some(CredentialRejected(scope: "read-balance"))
        }
      }
      let decision = sign_in_check.check(headers, "x-session", missing, verify)
      assert decision == expected
      assert process.receive(audit, within: 0) == Ok(audit_event)
      assert process.receive(audit, within: 0) == Error(Nil)
    },
  )
}
