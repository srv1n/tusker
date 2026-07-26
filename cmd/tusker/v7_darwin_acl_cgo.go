//go:build darwin && cgo

package main

/*
#include <errno.h>
#include <sys/acl.h>

static int tusker_fd_acl_has_mutation_grant(int fd, int *has_mutation) {
	acl_t acl;
	acl_entry_t entry;
	int entry_id = ACL_FIRST_ENTRY;
	int status;

	*has_mutation = 0;
	errno = 0;
	acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED);
	if (acl == NULL) {
		if (errno == ENOENT) {
			return 0;
		}
		return errno == 0 ? EIO : errno;
	}
	if (acl_valid(acl) != 0) {
		int saved = errno == 0 ? EIO : errno;
		acl_free(acl);
		return saved;
	}

	for (int count = 0; count < ACL_MAX_ENTRIES; count++) {
		acl_tag_t tag;
		acl_permset_mask_t perms;
		acl_permset_mask_t mutations =
		    ACL_WRITE_DATA | ACL_APPEND_DATA | ACL_DELETE |
		    ACL_DELETE_CHILD | ACL_WRITE_ATTRIBUTES |
		    ACL_WRITE_EXTATTRIBUTES | ACL_WRITE_SECURITY |
		    ACL_CHANGE_OWNER;
		errno = 0;
		status = acl_get_entry(acl, entry_id, &entry);
		if (status == -1) {
			if (errno == EINVAL) {
				acl_free(acl);
				return 0;
			}
			int saved = errno == 0 ? EIO : errno;
			acl_free(acl);
			return saved;
		}
		entry_id = ACL_NEXT_ENTRY;
		if (acl_get_tag_type(entry, &tag) != 0 ||
		    acl_get_permset_mask_np(entry, &perms) != 0) {
			int saved = errno == 0 ? EIO : errno;
			acl_free(acl);
			return saved;
		}
		if (tag != ACL_EXTENDED_ALLOW) {
			continue;
		}
		if ((perms & mutations) != 0) {
			*has_mutation = 1;
			acl_free(acl);
			return 0;
		}
	}
	acl_free(acl);
	return 0;
}
*/
import "C"

import (
	"fmt"
	"os"
	"syscall"
)

func v7DarwinDescriptorHasMutationACL(file *os.File) (bool, error) {
	if file == nil {
		return false, fmt.Errorf("nil descriptor")
	}
	var mutation C.int
	if errno := C.tusker_fd_acl_has_mutation_grant(C.int(file.Fd()), &mutation); errno != 0 {
		return false, syscall.Errno(errno)
	}
	return mutation != 0, nil
}
